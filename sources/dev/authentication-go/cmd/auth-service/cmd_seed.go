// Subcommand `auth-service seed`: bootstraps the admin user and the Admin
// Dashboard application client. Safe to re-run; existing records are reused.
package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/zhaochy1990/auth-service/internal/config"
	"github.com/zhaochy1990/auth-service/internal/seed"
	"github.com/zhaochy1990/auth-service/internal/storage"
)

func newSeedCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "seed [email] [password]",
		Short: "Bootstrap the admin user and Admin Dashboard application client",
		Args:  cobra.MaximumNArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			email := "admin@example.com"
			if len(args) > 0 {
				email = args[0]
			}
			var password *string
			if len(args) > 1 {
				password = &args[1]
			}
			return runSeed(context.Background(), email, password)
		},
	}
}

func runSeed(ctx context.Context, email string, password *string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	repo, err := storage.Open(ctx, cfg)
	if err != nil {
		return err
	}

	fmt.Println("=== Auth Service Bootstrap ===")
	fmt.Println()

	result, err := seed.Bootstrap(ctx, repo, email, password)
	if err != nil {
		return fmt.Errorf("bootstrap failed: %w", err)
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
	return nil
}
