// Command auth-service is the entrypoint for the Go auth microservice. It loads
// config from the environment, connects to Azure Table Storage, and either runs
// a subcommand (seed | migrate) or starts the Gin HTTP server.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/zhaochy1990/x/logger"

	"github.com/zhaochy1990/auth-service/internal/auth"
	"github.com/zhaochy1990/auth-service/internal/config"
	"github.com/zhaochy1990/auth-service/internal/repository/aztables"
	"github.com/zhaochy1990/auth-service/internal/seed"
	"github.com/zhaochy1990/auth-service/internal/server"
)

func main() {
	log := logger.MustGetLogger(&logger.LoggerConfig{
		Format:      config.EnvOr("LOG_FORMAT", "json"),
		ServiceName: "auth-service",
		Level:       config.EnvOr("LOG_LEVEL", "debug"),
	}).Sugar()

	cfg, err := config.FromEnv()
	if err != nil {
		log.Fatalw("failed to load configuration", "error", err)
	}

	ctx := context.Background()
	log.Infow("connecting to Azure Table Storage")
	repo, err := aztables.New(cfg.AzureStorageConnectionString)
	if err != nil {
		log.Fatalw("failed to connect to storage", "error", err)
	}
	if err := repo.EnsureTables(ctx); err != nil {
		log.Fatalw("failed to ensure tables", "error", err)
	}
	log.Infow("Azure Table Storage ready")

	args := os.Args
	if len(args) > 1 && args[1] == "seed" {
		runSeed(ctx, repo, args)
		return
	}
	if len(args) > 1 && args[1] == "migrate" {
		runMigrate(ctx, repo)
		return
	}

	jwt, err := auth.NewJWTManager(cfg)
	if err != nil {
		log.Fatalw("failed to initialize JWT manager", "error", err)
	}

	r := server.NewRouter(repo, jwt, cfg)
	log.Infow("starting server", "addr", cfg.Addr())
	if err := r.Run(cfg.Addr()); err != nil {
		log.Fatalw("server exited", "error", err)
	}
}

func runSeed(ctx context.Context, repo *aztables.Repository, args []string) {
	email := "admin@example.com"
	if len(args) > 2 {
		email = args[2]
	}
	var password *string
	if len(args) > 3 {
		password = &args[3]
	}

	fmt.Println("=== Auth Service Bootstrap ===")
	fmt.Println()

	result, err := seed.Bootstrap(ctx, repo, email, password)
	if err != nil {
		fmt.Println("bootstrap failed:", err)
		os.Exit(1)
	}

	fmt.Printf("  Client ID: %s\n", result.AppClientID)
	if result.AppClientSecret != nil {
		fmt.Printf("  Client Secret: %s\n", *result.AppClientSecret)
		fmt.Println("  (Save this secret — it won't be shown again!)")
	} else {
		fmt.Println("  Admin Dashboard application already exists.")
	}
	fmt.Println()

	switch result.UserAction {
	case "created":
		fmt.Printf("Created admin user: %s\n", email)
	case "promoted":
		fmt.Printf("Promoted %s to admin role.\n", email)
	case "already_admin":
		fmt.Printf("User %s is already an admin.\n", email)
	}

	fmt.Println()
	fmt.Println("=== Bootstrap complete ===")
	fmt.Println()
	fmt.Println("For frontend .env, set:")
	fmt.Printf("  VITE_API_CLIENT_ID=%s\n", result.AppClientID)
}

func runMigrate(ctx context.Context, repo *aztables.Repository) {
	fmt.Println("=== Auth Service Migration ===")
	fmt.Println()
	kinds, err := repo.MigrateInviteCodeKinds(ctx)
	if err != nil {
		fmt.Println("migration failed:", err)
		os.Exit(1)
	}
	fmt.Printf("  Invite codes backfilled with `kind`: %d\n", kinds)
	users, err := repo.MigrateUserInviteCodes(ctx)
	if err != nil {
		fmt.Println("migration failed:", err)
		os.Exit(1)
	}
	fmt.Printf("  Users backfilled with `invite_code`: %d\n", users)
	sortIndexes, err := repo.MigrateUserSortIndexes(ctx)
	if err != nil {
		fmt.Println("migration failed:", err)
		os.Exit(1)
	}
	fmt.Printf("  Users indexed for admin list sorting: %d\n", sortIndexes)
	fmt.Println()
	fmt.Println("=== Migration complete ===")
}
