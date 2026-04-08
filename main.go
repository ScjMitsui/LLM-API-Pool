package main

import (
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

func main() {
	rand.Seed(time.Now().UnixNano())
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)

	if err := LoadConfig("config.yaml"); err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	InitState()

	go GlobalRegistry.RefreshLoop()

	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	
	// Add static dir
	r.StaticFS("/static", http.Dir("./static"))

	// API Routes
	v1 := r.Group("/v1")
	{
		v1.POST("/chat/completions", ProxyHandler)
		v1.POST("/completions", ProxyHandler)
		v1.POST("/embeddings", ProxyHandler)
		v1.GET("/models", ListModels)
	}

	// Admin Routes
	admin := r.Group("/admin")
	{
		admin.GET("", func(c *gin.Context) {
			c.File("./static/index.html")
		})
		admin.GET("/endpoints", AdminList)
		admin.POST("/endpoints", AdminAdd)
		admin.DELETE("/endpoints/:name", AdminRemove)
		admin.PATCH("/endpoints/:name", AdminToggle)
		admin.GET("/models", AdminModels)
		admin.POST("/models/refresh", AdminModelsRefresh)
		admin.GET("/aliases", AdminAliases)
		admin.POST("/aliases", AdminSetAliases)
		admin.POST("/save", AdminSave)
		admin.POST("/stats/reset", AdminStatsReset)
		admin.POST("/endpoints/:name/clear_error", AdminClearError)
		admin.GET("/log", AdminLog)
		admin.POST("/restart", AdminRestart)
	}

	addr := fmt.Sprintf("%s:%d", AppConfig.Server.Host, AppConfig.Server.Port)
	log.Printf("🚀 Highly-Concurrent Go Pool proxy started on %s\n", addr)
	r.Run(addr)
} 
