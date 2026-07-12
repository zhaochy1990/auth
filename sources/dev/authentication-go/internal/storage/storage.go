// Package storage wires runtime configuration to a concrete repository adapter.
package storage

import (
	"context"
	"fmt"

	"github.com/zhaochy1990/auth-service/internal/config"
	"github.com/zhaochy1990/auth-service/internal/repository"
	"github.com/zhaochy1990/auth-service/internal/repository/aztables"
	mysqlrepo "github.com/zhaochy1990/auth-service/internal/repository/mysql"
)

// Open initializes the configured repository backend.
func Open(ctx context.Context, cfg *config.Config) (repository.Repository, error) {
	switch cfg.StorageBackend {
	case config.StorageBackendAzureTable:
		repo, err := aztables.New(cfg.AzureStorageConnectionString)
		if err != nil {
			return nil, err
		}
		if err := repo.EnsureTables(ctx); err != nil {
			return nil, err
		}
		return repo, nil
	case config.StorageBackendMySQL:
		return mysqlrepo.NewWithOptions(ctx, cfg.MySQLDSN, mysqlrepo.Options{TLSCAPEM: cfg.MySQLTLSCAPEM, TLSCAPath: cfg.MySQLTLSCAPath})
	default:
		return nil, fmt.Errorf("unsupported storage backend %q", cfg.StorageBackend)
	}
}
