// Subcommand `auth-service serve`: loads the YAML config file (--config,
// $CONFIG_PATH, or /etc/viper.yml) with environment overrides, opens the
// configured storage backend, and starts the Gin HTTP server. This is the
// default runtime entrypoint for the container.
package main

import (
	"context"

	"github.com/spf13/cobra"
	"github.com/zhaochy1990/x/logger"

	"github.com/zhaochy1990/auth-service/internal/auth"
	"github.com/zhaochy1990/auth-service/internal/config"
	"github.com/zhaochy1990/auth-service/internal/repository/redis"
	"github.com/zhaochy1990/auth-service/internal/server"
	"github.com/zhaochy1990/auth-service/internal/sms"
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
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}

	logger.MustGetLogger(&logger.LoggerConfig{
		Format:      cfg.LogFormat,
		ServiceName: "auth-service",
		Level:       cfg.LogLevel,
	}).Sugar()

	ctx := context.Background()

	logger.S().Info("opening MySQL storage backend")
	repo, err := storage.Open(ctx, cfg)
	if err != nil {
		return err
	}
	logger.S().Info("storage ready")

	jwt, err := auth.NewJWTManager(cfg)
	if err != nil {
		return err
	}

	// The Redis code store connects lazily: the service boots even when Redis
	// is unreachable, and the SMS endpoints fail closed (503) per request.
	smsStore := redis.New(cfg.RedisAddr, cfg.RedisPassword, cfg.RedisDB)
	smsClient := sms.NewClient(sms.Config{
		SecretID:   cfg.TencentSMSSecretID,
		SecretKey:  cfg.TencentSMSSecretKey,
		SDKAppID:   cfg.TencentSMSSDKAppID,
		SignName:   cfg.TencentSMSSignName,
		TemplateID: cfg.TencentSMSTemplateID,
		Region:     cfg.TencentSMSRegion,
	}, "")

	r := server.NewRouter(repo, jwt, cfg, smsStore, smsClient)
	logger.S().Infow("starting server", "addr", cfg.Addr(), "swagger_enabled", cfg.SwaggerEnabled)
	if cfg.SwaggerEnabled {
		logger.S().Infow("swagger UI available", "path", "/swagger/index.html")
	}
	return r.Run(cfg.Addr())
}
