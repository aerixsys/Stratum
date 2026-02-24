package server

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stratum/gateway/internal/bedrock"
	"github.com/stratum/gateway/internal/config"
	"github.com/stratum/gateway/internal/handler"
	"github.com/stratum/gateway/internal/middleware"
	"github.com/stratum/gateway/internal/service"
)

var (
	newBedrockClient = bedrock.NewClient
	notifyContext    = signal.NotifyContext
)

// Run starts the Stratum server.
func Run(cfg *config.Config) error {
	// Create Bedrock client
	client, err := newBedrockClient(cfg)
	if err != nil {
		return fmt.Errorf("bedrock client: %w", err)
	}

	// Model cache with 5 min TTL
	modelCache := bedrock.NewModelCache(client, 5*time.Minute)

	// Pre-load models
	if _, err := modelCache.GetModels(context.Background()); err != nil {
		log.Printf("[warn] Initial model discovery failed: %v", err)
	}

	// Translate config
	translateCfg := bedrock.TranslateConfig{
		EnablePromptCaching:    cfg.EnablePromptCaching,
		AllowPrivateImageFetch: cfg.AllowPrivateImageFetch,
		ImageMaxBytes:          cfg.ImageMaxBytes,
		ImageFetchTimeout:      time.Duration(cfg.ImageFetchTimeoutSeconds) * time.Second,
	}

	// Create services
	modelsService := service.NewModelsService(modelCache)
	chatService := service.NewChatService(client, modelCache, translateCfg, cfg.DefaultModel)
	embeddingsService := service.NewEmbeddingsService(client, cfg.DefaultEmbeddingModel)

	// Create handlers
	chatHandler := handler.NewChatHandler(chatService)
	modelsHandler := handler.NewModelsHandler(modelsService)
	embeddingsHandler := handler.NewEmbeddingsHandler(embeddingsService)

	// Gin setup
	if cfg.LogLevel != "debug" {
		gin.SetMode(gin.ReleaseMode)
	}
	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(middleware.RequestID())
	router.Use(requestLogger(cfg.LogLevel))
	router.Use(corsMiddleware(cfg))
	router.Use(middleware.BodyLimit(cfg.MaxRequestBodyBytes))
	rateLimiter := middleware.NewRateLimiter(cfg.RateLimitRPM, cfg.RateLimitBurst)
	router.Use(rateLimiter.Middleware())

	metrics := newMetricsCollector()
	if cfg.EnableMetrics {
		router.Use(metrics.middleware())
	}

	// Public routes
	router.GET("/health", handler.HealthHandler)
	router.GET("/ready", handler.ReadyHandler)
	if cfg.EnableMetrics {
		router.GET("/metrics", metrics.handler)
	}

	// Protected v1 routes
	v1 := router.Group("/v1")
	v1.Use(middleware.APIKeyAuth(cfg.APIKey))
	{
		v1.GET("/models", modelsHandler.Handle)
		v1.GET("/models/:id", modelsHandler.HandleGet)
		v1.POST("/chat/completions", chatHandler.Handle)
		v1.POST("/embeddings", embeddingsHandler.Handle)
	}

	// Also support /api/v1 prefix for backward compat
	apiV1 := router.Group("/api/v1")
	apiV1.Use(middleware.APIKeyAuth(cfg.APIKey))
	{
		apiV1.GET("/models", modelsHandler.Handle)
		apiV1.GET("/models/:id", modelsHandler.HandleGet)
		apiV1.POST("/chat/completions", chatHandler.Handle)
		apiV1.POST("/embeddings", embeddingsHandler.Handle)
	}

	// HTTP server
	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      7 * time.Minute,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	// Graceful shutdown
	ctx, stop := notifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		printBanner(cfg)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server failed: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("Shutting down...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("Shutdown error: %v", err)
	}

	return nil
}

func printBanner(cfg *config.Config) {
	fmt.Println()
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println("  Stratum Gateway")
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Printf("  Port:           %s\n", cfg.Port)
	fmt.Printf("  Region:         %s\n", cfg.AWSRegion)
	fmt.Printf("  Cross-Region:   %v\n", cfg.EnableCrossRegion)
	fmt.Printf("  Prompt Cache:   %v\n", cfg.EnablePromptCaching)
	fmt.Printf("  Log Level:      %s\n", cfg.LogLevel)
	fmt.Printf("  API:            http://localhost:%s/v1\n", cfg.Port)
	fmt.Println("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━")
	fmt.Println()
}

func corsMiddleware(cfg *config.Config) gin.HandlerFunc {
	allowed := make(map[string]struct{}, len(cfg.AllowedOrigins))
	for _, o := range cfg.AllowedOrigins {
		allowed[strings.TrimSpace(o)] = struct{}{}
	}

	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if cfg.AllowAnyOrigin {
			c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		} else if origin != "" {
			if _, ok := allowed[origin]; ok {
				c.Writer.Header().Set("Access-Control-Allow-Origin", origin)
				c.Writer.Header().Set("Vary", "Origin")
			}
		}
		c.Writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		c.Writer.Header().Set("Access-Control-Max-Age", "86400")

		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}

func requestLogger(level string) gin.HandlerFunc {
	if level == "debug" {
		return gin.Logger()
	}
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		latencyMs := time.Since(start).Milliseconds()
		reqID := middleware.GetRequestID(c)
		model, _ := c.Get("model")
		errType, _ := c.Get("error_type")
		log.Printf("request_id=%s method=%s path=%s status=%d latency_ms=%d model=%v error_type=%v",
			reqID,
			c.Request.Method,
			c.Request.URL.Path,
			c.Writer.Status(),
			latencyMs,
			model,
			errType,
		)
	}
}
