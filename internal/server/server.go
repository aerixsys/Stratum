package server

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stratum/gateway/internal/bedrock"
	"github.com/stratum/gateway/internal/config"
	"github.com/stratum/gateway/internal/handler"
	"github.com/stratum/gateway/internal/logging"
	"github.com/stratum/gateway/internal/middleware"
	"github.com/stratum/gateway/internal/policy"
	"github.com/stratum/gateway/internal/service"
)

var (
	newBedrockClient = bedrock.NewClient
	notifyContext    = signal.NotifyContext
)

// Run starts the Stratum server.
func Run(cfg *config.Config) error {
	modelPolicy, err := policy.LoadDefaultModelPolicy(cfg.ModelPolicyPath)
	if err != nil {
		return fmt.Errorf("model policy: %w", err)
	}

	// Create Bedrock client
	client, err := newBedrockClient(cfg)
	if err != nil {
		return fmt.Errorf("bedrock client: %w", err)
	}

	// Model cache with 5 min TTL
	modelCache := bedrock.NewModelCache(client, 5*time.Minute)
	modelDiscovery := bedrock.NewPolicyFilteredDiscovery(modelCache, modelPolicy)

	// Pre-load models
	if _, err := modelDiscovery.GetModels(context.Background()); err != nil {
		logging.Warnf("initial model discovery failed: %v", err)
	}

	// Create services
	modelsService := service.NewModelsService(modelDiscovery, modelPolicy)
	chatService := service.NewChatService(client, modelDiscovery, modelPolicy)

	// Create handlers
	chatHandler := handler.NewChatHandler(chatService)
	modelsHandler := handler.NewModelsHandler(modelsService)

	// Gin setup
	if cfg.LogLevel != "debug" {
		gin.SetMode(gin.ReleaseMode)
	}
	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(middleware.RequestContext(middleware.RequestContextOptions{
		AccessLogEnabled: cfg.LogLevel == "debug",
	}))
	router.Use(corsMiddleware())
	router.Use(middleware.BodyLimit(cfg.MaxRequestBodyBytes))

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
	serveErrCh := make(chan error, 1)

	go func() {
		printBanner(cfg)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			select {
			case serveErrCh <- err:
			default:
			}
		}
	}()

	select {
	case <-ctx.Done():
	case err := <-serveErrCh:
		return fmt.Errorf("server failed: %w", err)
	}

	logging.Infof("shutdown signal received")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logging.Warnf("shutdown error: %v", err)
	}

	return nil
}

func printBanner(cfg *config.Config) {
	logging.Banner([]string{
		"Stratum Gateway",
		fmt.Sprintf("Port:           %s", cfg.Port),
		fmt.Sprintf("Region:         %s", cfg.AWSRegion),
		fmt.Sprintf("Log Level:      %s", cfg.LogLevel),
		fmt.Sprintf("API:            http://localhost:%s/v1", cfg.Port),
	})
}

func corsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
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
