package main

import (
	"encoding/json"
	"io"
	"log"
	"math/rand"
	"net/http"
	"sync"
	"time"
)

type Endpoint struct {
	Name            string   `json:"name"`
	APIBase         string   `json:"api_base"`
	APIKey          string   `json:"-"`
	Enabled         bool     `json:"enabled"`
	ManualModels    []string `json:"-"`
	Healthy         bool     `json:"healthy"`
	Failures        int      `json:"consecutive_failures"`
	LastFailure     time.Time`json:"-"`
	TotalRequests   int      `json:"total_requests"`
	Successful      int      `json:"successful"`
	Failed          int      `json:"failed"`
	LastUsed        string   `json:"last_used"`
	LastError       *string  `json:"last_error"`
	LastErrorTime   *string  `json:"last_error_time"`
	LastUsedModel   *string  `json:"last_used_model"`
}

type ModelRegistry struct {
	mu        sync.RWMutex
	epModels  map[string]map[string]bool
	modelMap  map[string]map[string]bool
	client    *http.Client
}

type LatestReply struct {
	Endpoint *string `json:"endpoint"`
	Model    *string `json:"model"`
	Time     *string `json:"time"`
}

type Pool struct {
	mu           sync.RWMutex
	Endpoints    []*Endpoint
	Strategy     string
	Cooldown     time.Duration
	MaxRetries   int
	Index        int
	LatestReply  LatestReply
}

var (
	GlobalPool     = &Pool{}
	GlobalRegistry = &ModelRegistry{
		epModels: make(map[string]map[string]bool),
		modelMap: make(map[string]map[string]bool),
		client: &http.Client{Timeout: 10 * time.Second},
	}
	GlobalRequestLog = &RequestLog{MaxSize: 50}
)

func InitState() {
	cfgLock.RLock()
	defer cfgLock.RUnlock()
	
	GlobalPool.Strategy = AppConfig.Pool.Strategy
	GlobalPool.Cooldown = time.Duration(AppConfig.Pool.HealthCheckCooldown) * time.Second
	GlobalPool.MaxRetries = AppConfig.Pool.MaxRetries
	
	GlobalPool.mu.Lock()
	GlobalPool.Endpoints = make([]*Endpoint, 0)
	for _, ec := range AppConfig.Endpoints {
		GlobalPool.Endpoints = append(GlobalPool.Endpoints, &Endpoint{
			Name:         ec.Name,
			APIBase:      ec.APIBase,
			APIKey:       ec.APIKey,
			Enabled:      ec.Enabled,
			ManualModels: ec.Models,
			Healthy:      true,
		})
	}
	GlobalPool.mu.Unlock()
}

func (p *Pool) RecordSuccess(ep *Endpoint, model string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	ep.TotalRequests++
	ep.Successful++
	ep.Failures = 0
	ep.Healthy = true

	now := time.Now().Format("2006-01-02T15:04:05")
	ep.LastUsed = now
	if model != "" {
		m := model
		ep.LastUsedModel = &m
	}
	
	epName := ep.Name
	p.LatestReply = LatestReply{
		Endpoint: &epName,
		Model:    ep.LastUsedModel,
		Time:     &now,
	}

	GlobalRequestLog.Append(RequestLogEntry{
		Time:     now,
		Endpoint: ep.Name,
		Model:    model,
		Status:   "success",
	})
}

func (p *Pool) RecordFailure(ep *Endpoint, errMsg string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	ep.TotalRequests++
	ep.Failed++
	ep.Failures++
	ep.LastFailure = time.Now()

	now := time.Now().Format("2006-01-02T15:04:05")
	ep.LastUsed = now
	msg := errMsg
	ep.LastError = &msg
	ep.LastErrorTime = &now

	if ep.Failures >= 2 {
		ep.Healthy = false
		log.Printf("⛔ %s marked unhealthy\n", ep.Name)
	}

	GlobalRequestLog.Append(RequestLogEntry{
		Time:     now,
		Endpoint: ep.Name,
		Model:    "",
		Status:   "error",
		Error:    errMsg,
	})
}

func (p *Pool) Recover() {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now()
	for _, ep := range p.Endpoints {
		if !ep.Healthy && now.Sub(ep.LastFailure) >= p.Cooldown {
			ep.Healthy = true
			ep.Failures = 0
		}
	}
}

func (p *Pool) Next(requestedModel string) (*Endpoint, string) {
	return p.NextExcluding(requestedModel, nil)
}

func (p *Pool) NextExcluding(requestedModel string, exclude map[string]bool) (*Endpoint, string) {
	p.Recover()
	
	p.mu.Lock()
	defer p.mu.Unlock()

	var avail []*Endpoint
	for _, ep := range p.Endpoints {
		if ep.Enabled && ep.Healthy && !exclude[ep.Name] {
			avail = append(avail, ep)
		}
	}

	if len(avail) == 0 {
		// Panic recovery mode — only for endpoints not in the exclude set
		for _, ep := range p.Endpoints {
			if ep.Enabled && !exclude[ep.Name] {
				ep.Healthy = true
				ep.Failures = 0
				avail = append(avail, ep)
			}
		}
	}
	if len(avail) == 0 {
		return nil, ""
	}

	// 1. If explicit targets are provided via the ModelAliases map, strictly use those instead of filtering
	cfgLock.RLock()
	mappedTargets, ok := AppConfig.ModelAliases[requestedModel]
	cfgLock.RUnlock()

	if ok && len(mappedTargets) > 0 {
		var specificAvail []PoolTarget
		// Ensure the endpoint associated with the target is actually enabled, healthy, and not excluded
		for _, target := range mappedTargets {
			for _, ep := range avail {
				if ep.Name == target.Endpoint {
					specificAvail = append(specificAvail, target)
					break
				}
			}
		}

		if len(specificAvail) > 0 {
			var pickedTarget PoolTarget
			switch p.Strategy {
			case "random":
				pickedTarget = specificAvail[rand.Intn(len(specificAvail))]
			case "weighted":
				// Build weighted selection over targets using their endpoint's success rate
				const floor = 0.05
				weights := make([]float64, len(specificAvail))
				total := 0.0
				for i, t := range specificAvail {
					for _, ep := range avail {
						if ep.Name == t.Endpoint {
							w := successRate(ep) + floor
							weights[i] = w
							total += w
							break
						}
					}
				}
				r := rand.Float64() * total
				for i, w := range weights {
					r -= w
					if r <= 0 {
						pickedTarget = specificAvail[i]
						break
					}
				}
				if pickedTarget.Endpoint == "" {
					pickedTarget = specificAvail[len(specificAvail)-1]
				}
			default: // round_robin
				p.Index %= len(specificAvail)
				pickedTarget = specificAvail[p.Index]
				p.Index++
			}

			// Find the actual endpoint struct for that chosen target
			for _, ep := range avail {
				if ep.Name == pickedTarget.Endpoint {
					return ep, pickedTarget.Model
				}
			}
		}
		
		// If we are here, none of the endpoints in the aliased Pool were available (or they were all excluded).
		// We MUST NOT leak the request to other outside endpoints.
		return nil, ""
	}

	// 2. Otherwise, default to generic load balancing without model filtering (blind bypass as requested)
	ep := p.pick(avail)
	return ep, requestedModel
}

func (p *Pool) PoolSize(requestedModel string) int {
	p.mu.RLock()
	defer p.mu.RUnlock()

	var avail []*Endpoint
	for _, ep := range p.Endpoints {
		if ep.Enabled && ep.Healthy {
			avail = append(avail, ep)
		}
	}

	cfgLock.RLock()
	mappedTargets, ok := AppConfig.ModelAliases[requestedModel]
	cfgLock.RUnlock()

	if ok && len(mappedTargets) > 0 {
		count := 0
		for _, target := range mappedTargets {
			for _, ep := range avail {
				if ep.Name == target.Endpoint {
					count++
					break
				}
			}
		}
		if count > 0 {
			return count
		}
	}
	return len(avail)
}

func successRate(ep *Endpoint) float64 {
	if ep.TotalRequests == 0 {
		return 1.0 // untested endpoints are assumed good
	}
	return float64(ep.Successful) / float64(ep.TotalRequests)
}

func (p *Pool) pickWeighted(candidates []*Endpoint) *Endpoint {
	const floor = 0.05
	weights := make([]float64, len(candidates))
	total := 0.0
	for i, ep := range candidates {
		w := successRate(ep) + floor
		weights[i] = w
		total += w
	}
	r := rand.Float64() * total
	for i, w := range weights {
		r -= w
		if r <= 0 {
			return candidates[i]
		}
	}
	return candidates[len(candidates)-1]
}

func (p *Pool) pick(candidates []*Endpoint) *Endpoint {
	switch p.Strategy {
	case "random":
		return candidates[rand.Intn(len(candidates))]
	case "weighted":
		return p.pickWeighted(candidates)
	default: // round_robin
		p.Index %= len(candidates)
		chosen := candidates[p.Index]
		p.Index++
		return chosen
	}
}
	
// Registry Logic Updates
func (r *ModelRegistry) RefreshLoop() {
	ttl := 300
	cfgLock.RLock()
	if AppConfig.Pool.ModelCacheTTL > 0 {
		ttl = AppConfig.Pool.ModelCacheTTL
	}
	cfgLock.RUnlock()

	r.Refresh()
	for {
		time.Sleep(time.Duration(ttl) * time.Second)
		r.Refresh()
	}
}

type ModelData struct {
	ID string `json:"id"`
}
type ModelsResp struct {
	Data []ModelData `json:"data"`
}

func (r *ModelRegistry) Refresh() {
	GlobalPool.mu.RLock()
	var active []*Endpoint
	for _, ep := range GlobalPool.Endpoints {
		if ep.Enabled {
			active = append(active, ep)
		}
	}
	GlobalPool.mu.RUnlock()

	type result struct {
		epName string
		models []string
	}
	ch := make(chan result, len(active))
	var wg sync.WaitGroup

	for _, ep := range active {
		wg.Add(1)
		go func(e *Endpoint) {
			defer wg.Done()
			req, _ := http.NewRequest("GET", e.APIBase+"/models", nil)
			req.Header.Set("Authorization", "Bearer "+e.APIKey)
			req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
			res, err := r.client.Do(req)
			
			var found []string
			found = append(found, e.ManualModels...)
			if err == nil {
				defer res.Body.Close()
				if res.StatusCode == 200 {
					var mr ModelsResp
					// Read the full body first for debugging
					bodyBytes, _ := io.ReadAll(res.Body)
					if err := json.Unmarshal(bodyBytes, &mr); err != nil {
					     l := len(bodyBytes)
					     if l > 200 { l = 200 }
					     log.Printf("Warning: Failed to parse models for %s. Error: %v. Body (first 200): %s", e.Name, err, string(bodyBytes[:l]))
					} else {
						for _, m := range mr.Data {
							found = append(found, m.ID)
						}
					}
				} else {
					log.Printf("Warning: Model discovery HTTP %d for %s", res.StatusCode, e.Name)
				}
			} else {
				log.Printf("Warning: Model discovery failed for %s: %v", e.Name, err)
			}
			ch <- result{epName: e.Name, models: found}
		}(ep)
	}
	wg.Wait()
	close(ch)

	r.mu.Lock()
	defer r.mu.Unlock()
	r.epModels = make(map[string]map[string]bool)
	r.modelMap = make(map[string]map[string]bool)
	
	for res := range ch {
		r.epModels[res.epName] = make(map[string]bool)
		for _, m := range res.models {
			r.epModels[res.epName][m] = true
			if r.modelMap[m] == nil {
				r.modelMap[m] = make(map[string]bool)
			}
			r.modelMap[m][res.epName] = true
		}
	}
	log.Printf("🔄 Model registry refreshed across %d endpoint(s)\n", len(active))
}

// RequestLog — in-memory ring buffer for recent request outcomes
type RequestLogEntry struct {
	Time     string `json:"time"`
	Endpoint string `json:"endpoint"`
	Model    string `json:"model"`
	Status   string `json:"status"` // "success" or "error"
	Error    string `json:"error,omitempty"`
}

type RequestLog struct {
	mu      sync.Mutex
	entries []RequestLogEntry
	MaxSize int
}

func (rl *RequestLog) Append(entry RequestLogEntry) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	rl.entries = append(rl.entries, entry)
	if len(rl.entries) > rl.MaxSize {
		rl.entries = rl.entries[len(rl.entries)-rl.MaxSize:]
	}
}

func (rl *RequestLog) Entries() []RequestLogEntry {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	out := make([]RequestLogEntry, len(rl.entries))
	copy(out, rl.entries)
	return out
}
