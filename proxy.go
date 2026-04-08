package main

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"

	"github.com/gin-gonic/gin"
	"github.com/goccy/go-json"
)

type ProxyRequest struct {
	Model  string `json:"model,omitempty"`
	Stream bool   `json:"stream,omitempty"`
}

// retryableWriter buffers response headers and only commits them to the real
// ResponseWriter when upstream returns success (< 400). Failed responses are
// silently discarded so the caller can retry without corrupting the connection.
// Streaming is fully supported: once committed, all writes pass through.
type retryableWriter struct {
	real      http.ResponseWriter
	flusher   http.Flusher
	headerBuf http.Header
	failed    bool
	committed bool
}

func newRetryableWriter(w http.ResponseWriter) *retryableWriter {
	rw := &retryableWriter{
		real:      w,
		headerBuf: make(http.Header),
	}
	if f, ok := w.(http.Flusher); ok {
		rw.flusher = f
	}
	return rw
}

func (rw *retryableWriter) Header() http.Header {
	if rw.committed {
		return rw.real.Header()
	}
	return rw.headerBuf
}

func (rw *retryableWriter) WriteHeader(code int) {
	if rw.committed || rw.failed {
		return
	}
	if code >= 400 {
		rw.failed = true
		return
	}
	// Success — copy buffered headers to real writer and commit
	realHeader := rw.real.Header()
	for k, vv := range rw.headerBuf {
		for _, v := range vv {
			realHeader.Add(k, v)
		}
	}
	rw.real.WriteHeader(code)
	rw.committed = true
}

func (rw *retryableWriter) Write(data []byte) (int, error) {
	if rw.failed {
		return len(data), nil
	}
	if !rw.committed {
		rw.WriteHeader(http.StatusOK)
	}
	if rw.committed {
		return rw.real.Write(data)
	}
	return len(data), nil
}

func (rw *retryableWriter) Flush() {
	if rw.committed && rw.flusher != nil {
		rw.flusher.Flush()
	}
}

func ProxyHandler(c *gin.Context) {
	bodyBytes, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(400, gin.H{"error": "Failed to read body"})
		return
	}

	var parsedBody ProxyRequest
	if len(bodyBytes) > 0 {
		_ = json.Unmarshal(bodyBytes, &parsedBody)
	}

	requestedModel := parsedBody.Model
	var lastErr string
	origPath := c.Request.URL.Path // preserve across retries

	maxAttempts := GlobalPool.PoolSize(requestedModel)
	if maxAttempts < 1 {
		maxAttempts = 1
	}

	// Track tried endpoint+model pairs (not just endpoints)
	triedPairs := make(map[string]bool)

	for i := 0; i < maxAttempts; i++ {
		ep, targetModel := GlobalPool.NextExcluding(requestedModel, triedPairs)
		if ep == nil {
			c.JSON(502, gin.H{"error": gin.H{
				"message": "No healthy endpoints available based on your pool rules. Last error: " + lastErr,
				"code":    502,
			}})
			return
		}

		triedPairs[pairKey(ep.Name, targetModel)] = true

		if i > 0 {
			log.Printf("⚠ Retry %d/%d for model %q: %s, now trying %s (model: %s)",
				i, maxAttempts-1, requestedModel, lastErr, ep.Name, targetModel)
		}

		currentBody := bodyBytes
		if len(bodyBytes) > 0 && targetModel != "" && targetModel != requestedModel {
			var fullBody map[string]interface{}
			if err := json.Unmarshal(bodyBytes, &fullBody); err == nil {
				fullBody["model"] = targetModel
				if modBytes, err := json.Marshal(fullBody); err == nil {
					currentBody = modBytes
				}
			}
		}

		targetURL, _ := url.Parse(ep.APIBase)
		proxy := httputil.NewSingleHostReverseProxy(targetURL)

		proxy.Director = func(req *http.Request) {
			req.URL.Scheme = targetURL.Scheme
			req.URL.Host = targetURL.Host
			req.Host = targetURL.Host
			req.URL.Path = targetURL.Path + origPath[len("/v1"):]
			req.Header.Set("Authorization", "Bearer "+ep.APIKey)
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
			req.Body = io.NopCloser(bytes.NewBuffer(currentBody))
			req.ContentLength = int64(len(currentBody))
		}

		var attemptFailed bool
		var clientDisconnected bool

		proxy.ErrorHandler = func(rw http.ResponseWriter, req *http.Request, e error) {
			if req.Context().Err() != nil && req.Context().Err().Error() == "context canceled" {
				clientDisconnected = true
				lastErr = "client disconnected"
				return
			}
			GlobalPool.RecordFailure(ep, e.Error())
			lastErr = e.Error()
			attemptFailed = true
		}

		proxy.ModifyResponse = func(resp *http.Response) error {
			if resp.StatusCode >= 400 {
				errBody, _ := io.ReadAll(resp.Body)
				resp.Body.Close()

				errMsg := fmt.Sprintf("HTTP %d", resp.StatusCode)
				if len(errBody) > 0 {
					var parsed struct {
						Error struct {
							Message string `json:"message"`
							Type    string `json:"type"`
						} `json:"error"`
					}
					if json.Unmarshal(errBody, &parsed) == nil && parsed.Error.Message != "" {
						errMsg = fmt.Sprintf("HTTP %d: %s", resp.StatusCode, parsed.Error.Message)
					} else {
						raw := string(errBody)
						if len(raw) > 150 {
							raw = raw[:150] + "…"
						}
						errMsg = fmt.Sprintf("HTTP %d: %s", resp.StatusCode, raw)
					}
				}

				GlobalPool.RecordFailure(ep, errMsg)
				lastErr = errMsg
				attemptFailed = true
				// Replace consumed body so the proxy can still write it
				resp.Body = io.NopCloser(bytes.NewBuffer(errBody))
				return nil
			}
			GlobalPool.RecordSuccess(ep, targetModel)
			resp.Header.Set("X-Pool-Endpoint", ep.Name)
			return nil
		}

		rw := newRetryableWriter(c.Writer)
		proxy.ServeHTTP(rw, c.Request)

		if clientDisconnected {
			return
		}

		if !attemptFailed && rw.committed {
			return // Success — response already written/streamed to client
		}
		// Failed — nothing was written to c.Writer, safe to retry
	}

	c.JSON(502, gin.H{"error": gin.H{
		"message": "All backend attempts failed. Last error: " + lastErr,
		"code":    502,
	}})
}

type ModelItem struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int    `json:"created"`
	OwnedBy string `json:"owned_by"`
}

func ListModels(c *gin.Context) {
	GlobalRegistry.mu.RLock()
	var models []ModelItem
	seen := make(map[string]bool)

	for m := range GlobalRegistry.modelMap {
		seen[m] = true
		models = append(models, ModelItem{ID: m, Object: "model", OwnedBy: "pool-proxy"})
	}
	GlobalRegistry.mu.RUnlock()

	cfgLock.RLock()
	for alias := range AppConfig.ModelAliases {
		if !seen[alias] {
			seen[alias] = true
			models = append(models, ModelItem{ID: alias, Object: "model", OwnedBy: "pool-proxy-alias"})
		}
	}
	cfgLock.RUnlock()

	c.JSON(200, gin.H{"object": "list", "data": models})
}
