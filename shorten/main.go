package main

import (
	"context"
	"html/template"
	"io"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/google/uuid"
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

type Template struct {
	tmpl *template.Template
}

func NewTemplate(parse string) (*Template, error) {
	parsedTmpl, err := template.ParseGlob(parse)
	if err != nil {
		return nil, err
	}

	return &Template{
		tmpl: parsedTmpl,
	}, nil
}

func (t *Template) Render(w io.Writer, name string, data interface{}, c echo.Context) error {
	return t.tmpl.ExecuteTemplate(w, name, data)
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	logger = zerolog.New(os.Stdout).With().Timestamp().Logger()
	server := echo.New()

	var err error
	server.Renderer, err = NewTemplate("./index.html")
	if err != nil {
		logger.Err(err).Msg("unable to load templates")
	}

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

	server.GET("/", home)
	server.POST("/shorten", shortenURL)

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

func home(c echo.Context) error {
	return c.Render(http.StatusOK, "index.html", nil)
}

func shortenURL(c echo.Context) error {
	url := c.FormValue("url")

	if url == "" {
		return c.String(http.StatusBadRequest, "url cannot be empty")
	}

	short := uuid.New().String()

	shortenUrl := "https://" + c.Request().Host + "/s/" + short

	if os.Getenv("DEV_FLAG") == "1" {
		shortenUrl = "http://" + c.Request().Host + "/s/" + short
	}

	if _, err := redisClient.Set(c.Request().Context(), short, url, 24*time.Hour).Result(); err != nil {
		logger.Err(err).Msg("failed to set shortened url")
		return c.String(http.StatusInternalServerError, "failed to set shortened url")
	}

	return c.String(http.StatusOK, shortenUrl)
}
