// Command auth-service is the entrypoint for the Go auth microservice. It loads
// config from the environment, opens the configured storage backend, and either
// runs a subcommand or starts the Gin HTTP server.
package main

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/zhaochy1990/x/logger"

	"github.com/zhaochy1990/auth-service/internal/auth"
	"github.com/zhaochy1990/auth-service/internal/config"
	"github.com/zhaochy1990/auth-service/internal/repository"
	"github.com/zhaochy1990/auth-service/internal/repository/aztables"
	mysqlrepo "github.com/zhaochy1990/auth-service/internal/repository/mysql"
	"github.com/zhaochy1990/auth-service/internal/seed"
	"github.com/zhaochy1990/auth-service/internal/server"
	"github.com/zhaochy1990/auth-service/internal/storage"
)

func main() {
	log := logger.MustGetLogger(&logger.LoggerConfig{
		Format:      config.EnvOr("LOG_FORMAT", "json"),
		ServiceName: "auth-service",
		Level:       config.EnvOr("LOG_LEVEL", "debug"),
	}).Sugar()

	args := os.Args
	ctx := context.Background()
	if len(args) > 1 && args[1] == "migrate-storage" {
		runMigrateStorage(ctx, args)
		return
	}

	cfg, err := config.FromEnv()
	if err != nil {
		log.Fatalw("failed to load configuration", "error", err)
	}

	log.Infow("opening storage backend", "backend", cfg.StorageBackend)
	repo, err := storage.Open(ctx, cfg)
	if err != nil {
		log.Fatalw("failed to open storage", "backend", cfg.StorageBackend, "error", err)
	}
	log.Infow("storage ready", "backend", cfg.StorageBackend)

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

func runSeed(ctx context.Context, repo repository.Repository, args []string) {
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

func runMigrate(ctx context.Context, repo repository.Repository) {
	azRepo, ok := repo.(*aztables.Repository)
	if !ok {
		fmt.Println("migrate is only needed for the legacy azure_table backend")
		return
	}
	fmt.Println("=== Auth Service Migration ===")
	fmt.Println()
	kinds, err := azRepo.MigrateInviteCodeKinds(ctx)
	if err != nil {
		fmt.Println("migration failed:", err)
		os.Exit(1)
	}
	fmt.Printf("  Invite codes backfilled with `kind`: %d\n", kinds)
	users, err := azRepo.MigrateUserInviteCodes(ctx)
	if err != nil {
		fmt.Println("migration failed:", err)
		os.Exit(1)
	}
	fmt.Printf("  Users backfilled with `invite_code`: %d\n", users)
	sortIndexes, err := azRepo.MigrateUserSortIndexes(ctx)
	if err != nil {
		fmt.Println("migration failed:", err)
		os.Exit(1)
	}
	fmt.Printf("  Users indexed for admin list sorting: %d\n", sortIndexes)
	fmt.Println()
	fmt.Println("=== Migration complete ===")
}

func runMigrateStorage(ctx context.Context, args []string) {
	if len(args) < 3 || args[2] != "azure-to-mysql" {
		fmt.Println("usage: auth-service migrate-storage azure-to-mysql [--dry-run] [--clear-target]")
		os.Exit(2)
	}
	dryRun := hasArg(args[3:], "--dry-run")
	clearTarget := hasArg(args[3:], "--clear-target")
	azureConn := os.Getenv("AZURE_STORAGE_CONNECTION_STRING")
	mysqlDSN := os.Getenv("MYSQL_DSN")
	if azureConn == "" {
		fmt.Println("AZURE_STORAGE_CONNECTION_STRING is required")
		os.Exit(2)
	}
	if mysqlDSN == "" && !dryRun {
		fmt.Println("MYSQL_DSN is required unless --dry-run is set")
		os.Exit(2)
	}

	source, err := aztables.New(azureConn)
	if err != nil {
		fmt.Println("failed to open Azure Tables source:", err)
		os.Exit(1)
	}
	data, err := source.ExportSnapshot(ctx)
	if err != nil {
		fmt.Println("failed to export Azure Tables snapshot:", err)
		os.Exit(1)
	}

	fmt.Println("=== Azure Tables -> MySQL Storage Migration ===")
	fmt.Println()
	printCounts("exported", data.Counts())
	if dryRun {
		fmt.Println()
		fmt.Println("Dry run complete; no MySQL rows were written.")
		return
	}

	target, err := mysqlrepo.New(ctx, mysqlDSN)
	if err != nil {
		fmt.Println("failed to open MySQL target:", err)
		os.Exit(1)
	}
	defer target.Close()
	if clearTarget {
		if err := target.ClearAllTables(ctx); err != nil {
			fmt.Println("failed to clear MySQL target:", err)
			os.Exit(1)
		}
	}
	if err := target.ImportSnapshot(ctx, *data); err != nil {
		fmt.Println("failed to import MySQL snapshot:", err)
		os.Exit(1)
	}
	fmt.Println()
	counts, err := target.SnapshotCounts(ctx)
	if err != nil {
		fmt.Println("failed to count MySQL target:", err)
		os.Exit(1)
	}
	printCounts("imported", counts)
	if err := compareCounts(data.Counts(), counts); err != nil {
		fmt.Println("migration verification failed:", err)
		os.Exit(1)
	}
	fmt.Println()
	fmt.Println("Import complete.")
}

func compareCounts(want, got map[string]int) error {
	keys := make([]string, 0, len(want))
	for key := range want {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if want[key] != got[key] {
			return fmt.Errorf("%s count mismatch: exported=%d imported=%d", key, want[key], got[key])
		}
	}
	return nil
}

func hasArg(args []string, want string) bool {
	for _, arg := range args {
		if arg == want {
			return true
		}
	}
	return false
}

func printCounts(label string, counts map[string]int) {
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	prefix := label
	if label != "" {
		prefix = strings.ToUpper(label[:1]) + label[1:]
	}
	for _, key := range keys {
		fmt.Printf("  %s %-18s %d\n", prefix, key+":", counts[key])
	}
}
