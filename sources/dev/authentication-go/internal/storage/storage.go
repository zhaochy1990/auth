// Package storage wires runtime configuration to the MySQL repository adapter.
package storage

import (
	"context"

	"github.com/zhaochy1990/auth-service/internal/config"
	"github.com/zhaochy1990/auth-service/internal/repository"
	mysqlrepo "github.com/zhaochy1990/auth-service/internal/repository/mysql"
)

// Open initializes the MySQL repository backend.
func Open(ctx context.Context, cfg *config.Config) (repository.Repository, error) {
	return mysqlrepo.NewWithOptions(ctx, cfg.MySQLDSN, mysqlrepo.Options{TLSCAPEM: cfg.MySQLTLSCAPEM, TLSCAPath: cfg.MySQLTLSCAPath})
}
