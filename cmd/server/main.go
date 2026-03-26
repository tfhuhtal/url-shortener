package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/tuomas/url-shortener/internal/config"
	httplogging "github.com/tuomas/url-shortener/internal/http"
	"github.com/tuomas/url-shortener/internal/shortener"
)

type shortenRequest struct {
	LongURL string `json:"longUrl" binding:"required,url"`
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	cfg := config.FromEnv()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pgPool, err := pgxpool.New(ctx, cfg.PostgresDSN())
	if err != nil {
		logger.Error("failed to create postgres pool", slog.Any("error", err))
		os.Exit(1)
	}
	defer pgPool.Close()

	if err := pgPool.Ping(ctx); err != nil {
		logger.Error("failed to ping postgres", slog.Any("error", err))
		os.Exit(1)
	}

	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.RedisAddr(),
		Password: cfg.RedisPassword,
		DB:       cfg.RedisDB,
	})
	defer func() {
		if closeErr := rdb.Close(); closeErr != nil {
			logger.Warn("failed to close redis client", slog.Any("error", closeErr))
		}
	}()

	if err := rdb.Ping(ctx).Err(); err != nil {
		logger.Error("failed to ping redis", slog.Any("error", err))
		os.Exit(1)
	}

	repo := shortener.NewRepository(pgPool)
	if err := repo.EnsureSchema(ctx); err != nil {
		logger.Error("failed to ensure schema", slog.Any("error", err))
		os.Exit(1)
	}

	service := shortener.NewService(repo, rdb)

	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(httplogging.SlogRequestLogger(logger))
	_ = router.SetTrustedProxies(nil)

	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	router.GET("/ping", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "pong"})
	})

	router.GET("/ready", func(c *gin.Context) {
		reqCtx, reqCancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer reqCancel()

		if err := pgPool.Ping(reqCtx); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "postgres unavailable", "error": err.Error()})
			return
		}

		if err := rdb.Ping(reqCtx).Err(); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "redis unavailable", "error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{"status": "ready"})
	})

	v1 := router.Group("/api/v1")

	v1.POST("/data/shorten", func(c *gin.Context) {
		var req shortenRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		record, err := service.Shorten(c.Request.Context(), req.LongURL)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to shorten url"})
			return
		}

		shortURL := fmt.Sprintf("%s://%s/api/v1/%s", requestScheme(c), c.Request.Host, record.ShortCode)
		c.JSON(http.StatusCreated, gin.H{
			"shortCode": record.ShortCode,
			"shortUrl":  shortURL,
			"longUrl":   record.LongURL,
		})
	})

	v1.GET("/:shortUrl", func(c *gin.Context) {
		shortCode := c.Param("shortUrl")
		longURL, err := service.Resolve(c.Request.Context(), shortCode)
		if err != nil {
			if errors.Is(err, shortener.ErrNotFound) {
				c.JSON(http.StatusNotFound, gin.H{"error": "short url not found"})
				return
			}

			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to resolve short url"})
			return
		}

		c.Redirect(http.StatusFound, longURL)
	})

	addr := fmt.Sprintf(":%s", cfg.Port)
	logger.Info("starting gin server", slog.String("address", addr))
	if err := router.Run(addr); err != nil {
		logger.Error("server exited with error", slog.Any("error", err))
		os.Exit(1)
	}
}

func requestScheme(c *gin.Context) string {
	if c.Request.TLS != nil {
		return "https"
	}

	if forwardedProto := c.GetHeader("X-Forwarded-Proto"); forwardedProto != "" {
		return forwardedProto
	}

	return "http"
}
