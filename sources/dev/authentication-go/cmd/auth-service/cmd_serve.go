// Subcommand `auth-service serve`: loads config from the environment, opens the
// configured storage backend, and starts the Gin HTTP server. This is the
// default runtime entrypoint for the container.
package main

import (
	"context"

	"github.com/spf13/cobra"
	"github.com/zhaochy1990/x/logger"

	"github.com/zhaochy1990/auth-service/internal/auth"
	"github.com/zhaochy1990/auth-service/internal/config"
	"github.com/zhaochy1990/auth-service/internal/server"
	"github.com/zhaochy1990/auth-service/internal/storage"
)

func newServeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Start the Gin HTTP server",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runServe()
		},
	}
}

func runServe() error {
	log := logger.MustGetLogger(&logger.LoggerConfig{
		Format:      config.EnvOr("LOG_FORMAT", "json"),
		ServiceName: "auth-service",
		Level:       config.EnvOr("LOG_LEVEL", "debug"),
	}).Sugar()

	ctx := context.Background()

	cfg, err := config.FromEnv()
	if err != nil {
		return err
	}

	log.Infow("opening storage backend", "backend", cfg.StorageBackend)
	repo, err := storage.Open(ctx, cfg)
	if err != nil {
		return err
	}
	log.Infow("storage ready", "backend", cfg.StorageBackend)

	jwt, err := auth.NewJWTManager(cfg)
	if err != nil {
		return err
	}

	r := server.NewRouter(repo, jwt, cfg)
	log.Infow("starting server", "addr", cfg.Addr(), "swagger_enabled", cfg.SwaggerEnabled)
	if cfg.SwaggerEnabled {
		log.Infow("swagger UI available", "path", "/swagger/index.html")
	}
	return r.Run(cfg.Addr())
}
