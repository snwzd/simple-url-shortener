package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
)

var (
	redisClient *redis.Client
	logger      zerolog.Logger
)

func init() {
	opts, err := redis.ParseURL(os.Getenv("REDIS_URI"))
	if err != nil {
		logger.Fatal().Err(err).Msg("invalid Redis URI")
	}

	redisClient = redis.NewClient(opts)

	_, err = redisClient.Ping(context.Background()).Result()
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to connect to Redis")
	}
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	logger = zerolog.New(os.Stdout).With().Timestamp().Logger()
	server := echo.New()

	server.Use(middleware.RequestLoggerWithConfig(middleware.RequestLoggerConfig{
		LogURI:    true,
		LogStatus: true,
		LogValuesFunc: func(c echo.Context, v middleware.RequestLoggerValues) error {
			logger.Info().
				Str("URI", v.URI).
				Int("status", v.Status).
				Msg("request")

			return nil
		},
	}))

	server.GET("/s/:id", redirectToURL)

	go func() {
		if err := server.Start(":" + os.Getenv("APP_PORT")); err != nil {
			logger.Err(err).Msg("failed to start the server")
			os.Exit(1)
		}
	}()

	<-ctx.Done()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		logger.Err(err).Msg("failed to gracefully shutdown the server")
		os.Exit(1)
	}
}

func redirectToURL(c echo.Context) error {
	shortURL := c.Param("id")

	if shortURL == "" {
		return c.String(http.StatusBadRequest, "short url cannot be empty")
	}

	url, err := redisClient.Get(c.Request().Context(), shortURL).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return c.String(http.StatusNotFound, "url not found")
		}

		logger.Err(err).Msg("failed to get shortened url")

		return c.String(http.StatusInternalServerError, "failed to retrieve url")
	}

	return c.Redirect(http.StatusFound, url)
}
