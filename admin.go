package main

import (
	"log"
	"os"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
)

func AdminList(c *gin.Context) {
	GlobalPool.mu.RLock()
	defer GlobalPool.mu.RUnlock()

	eps := make([]map[string]interface{}, 0, len(GlobalPool.Endpoints))
	for _, ep := range GlobalPool.Endpoints {
		eps = append(eps, map[string]interface{}{
			"name":                 ep.Name,
			"api_base":             ep.APIBase,
			"enabled":              ep.Enabled,
			"healthy":              ep.Healthy,
			"consecutive_failures": ep.Failures,
			"total_requests":       ep.TotalRequests,
			"successful":           ep.Successful,
			"failed":               ep.Failed,
			"last_used":            ep.LastUsed,
			"last_error":           ep.LastError,
			"last_error_time":      ep.LastErrorTime,
			"last_used_model":      ep.LastUsedModel,
		})
	}
	c.JSON(200, gin.H{
		"latest_reply": GlobalPool.LatestReply,
		"endpoints":    eps,
	})
}

func AdminAdd(c *gin.Context) {
	var body EndpointConfig
	if err := c.BindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": "invalid json"})
		return
	}
	
	newEp := &Endpoint{
		Name:    body.Name,
		APIBase: body.APIBase,
		APIKey:  body.APIKey,
		Enabled: body.Enabled,
		Healthy: true,
	}

	GlobalPool.mu.Lock()
	var updated []*Endpoint
	for _, e := range GlobalPool.Endpoints {
		if e.Name != newEp.Name {
			updated = append(updated, e)
		}
	}
	updated = append(updated, newEp)
	GlobalPool.Endpoints = updated
	GlobalPool.mu.Unlock()

	cfgLock.Lock()
	var newCfgList []EndpointConfig
	for _, e := range AppConfig.Endpoints {
		if e.Name != newEp.Name {
			newCfgList = append(newCfgList, e)
		}
	}
	newCfgList = append(newCfgList, body)
	AppConfig.Endpoints = newCfgList
	cfgLock.Unlock()

	SaveConfig("config.yaml")
	
	log.Printf("Added/updated endpoint: %s\n", newEp.Name)
	c.JSON(200, gin.H{"status": "added"})
}

func AdminRemove(c *gin.Context) {
	name := c.Param("name")
	
	GlobalPool.mu.Lock()
	var updated []*Endpoint
	for _, e := range GlobalPool.Endpoints {
		if e.Name != name {
			updated = append(updated, e)
		}
	}
	GlobalPool.Endpoints = updated
	GlobalPool.mu.Unlock()

	cfgLock.Lock()
	var newCfgList []EndpointConfig
	for _, e := range AppConfig.Endpoints {
		if e.Name != name {
			newCfgList = append(newCfgList, e)
		}
	}
	AppConfig.Endpoints = newCfgList
	cfgLock.Unlock()

	SaveConfig("config.yaml")

	c.JSON(200, gin.H{"status": "removed", "name": name})
}

func AdminToggle(c *gin.Context) {
	name := c.Param("name")
	type Req struct{ Enabled bool `json:"enabled"` }
	var body Req
	if err := c.BindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": "invalid"})
		return
	}

	GlobalPool.mu.Lock()
	for _, e := range GlobalPool.Endpoints {
		if e.Name == name {
			e.Enabled = body.Enabled
			e.Healthy = true
			e.Failures = 0
		}
	}
	GlobalPool.mu.Unlock()
	
	cfgLock.Lock()
	for i := range AppConfig.Endpoints {
		if AppConfig.Endpoints[i].Name == name {
			AppConfig.Endpoints[i].Enabled = body.Enabled
		}
	}
	cfgLock.Unlock()
	
	SaveConfig("config.yaml")

	c.JSON(200, gin.H{"status": "updated"})
}

func AdminStatsReset(c *gin.Context) {
	GlobalPool.mu.Lock()
	for _, ep := range GlobalPool.Endpoints {
		ep.TotalRequests = 0
		ep.Successful = 0
		ep.Failed = 0
		ep.LastUsed = ""
		ep.LastError = nil
		ep.LastErrorTime = nil
		ep.LastUsedModel = nil
	}
	GlobalPool.LatestReply = LatestReply{}
	GlobalPool.mu.Unlock()
	c.JSON(200, gin.H{"status": "reset"})
}

func AdminClearError(c *gin.Context) {
	name := c.Param("name")
	GlobalPool.mu.Lock()
	for _, ep := range GlobalPool.Endpoints {
		if ep.Name == name {
			ep.LastError = nil
			ep.LastErrorTime = nil
		}
	}
	GlobalPool.mu.Unlock()
	c.JSON(200, gin.H{"status": "cleared"})
}

func AdminModels(c *gin.Context) {
	GlobalRegistry.mu.RLock()
	defer GlobalRegistry.mu.RUnlock()
	
	md := make(map[string][]string)
	for m, eps := range GlobalRegistry.modelMap {
		var epList []string
		for ep := range eps {
			epList = append(epList, ep)
		}
		md[m] = epList
	}
	
	c.JSON(200, gin.H{
		"total_models": len(md),
		"models":       md,
	})
}

func AdminAliases(c *gin.Context) {
	cfgLock.RLock()
	defer cfgLock.RUnlock()
	c.JSON(200, gin.H{"aliases": AppConfig.ModelAliases})
}

// ... Additional simple handlers ...

type SetAliasesReq struct {
	Aliases map[string][]PoolTarget `json:"aliases"`
}

func AdminSetAliases(c *gin.Context) {
	var body SetAliasesReq
	if err := c.BindJSON(&body); err != nil {
		c.JSON(400, gin.H{"error": "invalid json"})
		return
	}

	cfgLock.Lock()
	if body.Aliases == nil {
		AppConfig.ModelAliases = make(map[string][]PoolTarget)
	} else {
		AppConfig.ModelAliases = body.Aliases
	}
	cfgLock.Unlock()

	SaveConfig("config.yaml")

	c.JSON(200, gin.H{
		"status":  "updated",
		"aliases": body.Aliases,
	})
}

func AdminSave(c *gin.Context) {
	err := SaveConfig("config.yaml")
	if err != nil {
		c.JSON(500, gin.H{"error": "Failed to save config"})
		return
	}
	c.JSON(200, gin.H{"status": "saved"})
}

func AdminModelsRefresh(c *gin.Context) {
	GlobalRegistry.Refresh()
	
	GlobalRegistry.mu.RLock()
	total := len(GlobalRegistry.modelMap)
	GlobalRegistry.mu.RUnlock()
	
	c.JSON(200, gin.H{
		"status": "refreshed",
		"total_models": total,
	})
}

func AdminLog(c *gin.Context) {
	entries := GlobalRequestLog.Entries()
	c.JSON(200, gin.H{"entries": entries})
}

func AdminRestart(c *gin.Context) {
	SaveConfig("config.yaml")
	c.JSON(200, gin.H{"status": "restarting"})

	go func() {
		time.Sleep(500 * time.Millisecond)
		exe, err := os.Executable()
		if err != nil {
			log.Printf("❌ Restart failed: cannot find executable: %v\n", err)
			return
		}
		log.Println("🔄 Restarting service...")
		err = syscall.Exec(exe, os.Args, os.Environ())
		if err != nil {
			log.Printf("❌ Restart failed: %v\n", err)
		}
	}()
}
