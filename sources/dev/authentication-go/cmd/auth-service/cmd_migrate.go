// Subcommand `auth-service migrate`: backfills legacy Azure Table rows (invite
// code kinds, user invite codes, admin-list sort indexes). It is a no-op on the
// MySQL backend and only needed during the Azure Tables cutover window.
package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/zhaochy1990/auth-service/internal/config"
	"github.com/zhaochy1990/auth-service/internal/repository/aztables"
	"github.com/zhaochy1990/auth-service/internal/storage"
)

func newMigrateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "migrate",
		Short: "Backfill legacy Azure Table rows (no-op on the MySQL backend)",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runMigrate(context.Background())
		},
	}
}

func runMigrate(ctx context.Context) error {
	cfg, err := config.FromEnv()
	if err != nil {
		return err
	}
	repo, err := storage.Open(ctx, cfg)
	if err != nil {
		return err
	}

	azRepo, ok := repo.(*aztables.Repository)
	if !ok {
		fmt.Println("migrate is only needed for the legacy azure_table backend")
		return nil
	}
	fmt.Println("=== Auth Service Migration ===")
	fmt.Println()
	kinds, err := azRepo.MigrateInviteCodeKinds(ctx)
	if err != nil {
		return fmt.Errorf("migration failed: %w", err)
	}
	fmt.Printf("  Invite codes backfilled with `kind`: %d\n", kinds)
	users, err := azRepo.MigrateUserInviteCodes(ctx)
	if err != nil {
		return fmt.Errorf("migration failed: %w", err)
	}
	fmt.Printf("  Users backfilled with `invite_code`: %d\n", users)
	sortIndexes, err := azRepo.MigrateUserSortIndexes(ctx)
	if err != nil {
		return fmt.Errorf("migration failed: %w", err)
	}
	fmt.Printf("  Users indexed for admin list sorting: %d\n", sortIndexes)
	fmt.Println()
	fmt.Println("=== Migration complete ===")
	return nil
}
