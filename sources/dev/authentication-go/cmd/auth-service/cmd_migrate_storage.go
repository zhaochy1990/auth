// Subcommand `auth-service migrate-storage azure-to-mysql`: exports every table
// from the legacy Azure Tables backend and imports it into MySQL, used for the
// one-time storage cutover. Reads its endpoints from the environment
// (AZURE_STORAGE_CONNECTION_STRING, MYSQL_DSN, MYSQL_TLS_CA_*).
package main

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/zhaochy1990/auth-service/internal/config"
	"github.com/zhaochy1990/auth-service/internal/repository/aztables"
	mysqlrepo "github.com/zhaochy1990/auth-service/internal/repository/mysql"
)

func newMigrateStorageCmd() *cobra.Command {
	var dryRun, clearTarget bool
	cmd := &cobra.Command{
		Use:   "migrate-storage azure-to-mysql",
		Short: "Copy the Azure Tables dataset into MySQL for the storage cutover",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if args[0] != "azure-to-mysql" {
				return fmt.Errorf("unsupported migration %q (only azure-to-mysql is supported)", args[0])
			}
			return runMigrateStorage(context.Background(), dryRun, clearTarget)
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "export and count only; write no MySQL rows")
	cmd.Flags().BoolVar(&clearTarget, "clear-target", false, "atomically replace a non-empty MySQL target during a planned cutover")
	return cmd
}

func runMigrateStorage(ctx context.Context, dryRun, clearTarget bool) error {
	azureConn := config.EnvOr("AZURE_STORAGE_CONNECTION_STRING", "")
	mysqlDSN := config.EnvOr("MYSQL_DSN", "")
	mysqlTLSCAPEM := config.EnvOr("MYSQL_TLS_CA_PEM", "")
	mysqlTLSCAPath := config.EnvOr("MYSQL_TLS_CA_PATH", "")
	if azureConn == "" {
		return fmt.Errorf("AZURE_STORAGE_CONNECTION_STRING is required")
	}
	if mysqlDSN == "" && !dryRun {
		return fmt.Errorf("MYSQL_DSN is required unless --dry-run is set")
	}

	source, err := aztables.New(azureConn)
	if err != nil {
		return fmt.Errorf("failed to open Azure Tables source: %w", err)
	}
	data, err := source.ExportSnapshot(ctx)
	if err != nil {
		return fmt.Errorf("failed to export Azure Tables snapshot: %w", err)
	}

	fmt.Println("=== Azure Tables -> MySQL Storage Migration ===")
	fmt.Println()
	printCounts("exported", data.Counts())
	if dryRun {
		fmt.Println()
		fmt.Println("Dry run complete; no MySQL rows were written.")
		return nil
	}

	target, err := mysqlrepo.NewWithOptions(ctx, mysqlDSN, mysqlrepo.Options{TLSCAPEM: mysqlTLSCAPEM, TLSCAPath: mysqlTLSCAPath})
	if err != nil {
		return fmt.Errorf("failed to open MySQL target: %w", err)
	}
	defer target.Close()
	if clearTarget {
		err = target.ReplaceWithSnapshot(ctx, *data)
	} else {
		counts, countErr := target.SnapshotCounts(ctx)
		if countErr != nil {
			return fmt.Errorf("failed to count MySQL target: %w", countErr)
		}
		if !countsEmpty(counts) {
			fmt.Println("MySQL target is not empty; use --clear-target for an atomic replacement during a planned cutover")
			printCounts("existing", counts)
			return fmt.Errorf("MySQL target is not empty")
		}
		err = target.ImportSnapshot(ctx, *data)
	}
	if err != nil {
		return fmt.Errorf("failed to import MySQL snapshot: %w", err)
	}
	fmt.Println()
	counts, err := target.SnapshotCounts(ctx)
	if err != nil {
		return fmt.Errorf("failed to count MySQL target: %w", err)
	}
	printCounts("imported", counts)
	if err := compareCounts(data.Counts(), counts); err != nil {
		return fmt.Errorf("migration verification failed: %w", err)
	}
	fmt.Println()
	fmt.Println("Import complete.")
	return nil
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

func countsEmpty(counts map[string]int) bool {
	for _, n := range counts {
		if n != 0 {
			return false
		}
	}
	return true
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
