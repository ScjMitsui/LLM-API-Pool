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

func ProxyHandler(c *gin.Context) {
	bodyBytes, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(400, gin.H{"error": "Failed to read body"})
		return
	}
	// Restore body for further reading
	c.Request.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

	var parsedBody ProxyRequest
	if len(bodyBytes) > 0 {
		_ = json.Unmarshal(bodyBytes, &parsedBody)
	}

	requestedModel := parsedBody.Model
	var lastErr string

	maxAttempts := GlobalPool.PoolSize(requestedModel)
	if maxAttempts < 1 {
		maxAttempts = 1
	}

	// Track which endpoints we've already tried so we don't re-pick them
	triedEndpoints := make(map[string]bool)

	for i := 0; i < maxAttempts; i++ {
		ep, targetModel := GlobalPool.NextExcluding(requestedModel, triedEndpoints)
		if ep == nil {
			c.JSON(502, gin.H{"error": gin.H{"message": "No healthy endpoints available based on your pool rules. Last error: " + lastErr, "code": 502}})
			return
		}

		triedEndpoints[ep.Name] = true

		if i > 0 {
			log.Printf("⚠ Retry %d/%d for model %q: endpoint %s failed, now trying %s",
				i, maxAttempts-1, requestedModel, lastErr, ep.Name)
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
		
		// Configure Director — manually set URL to avoid path doubling
		proxy.Director = func(req *http.Request) {
			req.URL.Scheme = targetURL.Scheme
			req.URL.Host = targetURL.Host
			req.Host = targetURL.Host
			// The incoming path is /v1/chat/completions and api_base already ends in /v1
			// So use the api_base path + just the suffix after /v1
			req.URL.Path = targetURL.Path + req.URL.Path[len("/v1"):]
			req.Header.Set("Authorization", "Bearer "+ep.APIKey)
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
			// Reset body to the possibly modified body
			req.Body = io.NopCloser(bytes.NewBuffer(currentBody))
			req.ContentLength = int64(len(currentBody))
		}

		// Create response catcher BEFORE handlers so closures can reference it
		cw := &responseCatcher{ResponseWriter: c.Writer}

		proxy.ErrorHandler = func(rw http.ResponseWriter, req *http.Request, e error) {
			GlobalPool.RecordFailure(ep, e.Error())
			lastErr = e.Error()
			cw.failed = true
		}

		proxy.ModifyResponse = func(resp *http.Response) error {
			if resp.StatusCode >= 400 {
				// Read and parse the actual error body from the upstream API
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
						// Fallback: use raw body (truncated)
						raw := string(errBody)
						if len(raw) > 150 {
							raw = raw[:150] + "…"
						}
						errMsg = fmt.Sprintf("HTTP %d: %s", resp.StatusCode, raw)
					}
				}

				GlobalPool.RecordFailure(ep, errMsg)
				lastErr = errMsg
				cw.failed = true
				return nil // We already handled the failure ourselves
			}
			GlobalPool.RecordSuccess(ep, targetModel)
			resp.Header.Set("X-Pool-Endpoint", ep.Name)
			return nil
		}

		proxy.ServeHTTP(cw, c.Request)

		if !cw.failed {
			return // Success
		}
		// Loop and try next endpoint
	}

	c.JSON(502, gin.H{"error": gin.H{"message": "All backend attempts failed. Last error: " + lastErr, "code": 502}})
}

type responseCatcher struct {
	gin.ResponseWriter
	failed bool
}

func (c *responseCatcher) WriteHeader(code int) {
	if code >= 400 {
		c.failed = true
		return
	}
	c.ResponseWriter.WriteHeader(code)
}

func (c *responseCatcher) Write(b []byte) (int, error) {
	if c.failed {
		return len(b), nil
	}
	return c.ResponseWriter.Write(b)
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
