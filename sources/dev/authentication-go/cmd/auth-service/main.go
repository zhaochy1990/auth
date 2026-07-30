// Command auth-service is the single entry point for the Go auth microservice.
// Every operation is a cobra subcommand of one binary (built once, deployed as
// different container entrypoints, e.g. `auth-service serve`):
//
//	auth-service serve              start the Gin HTTP server (default runtime)
//	auth-service seed [email] [pw]  bootstrap the admin user + dashboard app client
//	auth-service migrate            backfill legacy Azure Table rows (no-op on MySQL)
//	auth-service migrate-storage    copy Azure Tables data into MySQL for cutover
//
// Each subcommand stays thin: load config, open storage, run. All logic lives
// in internal/.
//
// The Swagger general API info below is attached to this file because
// `swag init -g cmd/auth-service/main.go` reads it from the -g entry package.
//
//	@title						Auth Service API
//	@version					1.0
//	@description				Authentication microservice: password/provider login, OAuth2 token issuance, user & team management, and admin operations.
//	@securityDefinitions.apikey	ClientID
//	@in							header
//	@name						X-Client-Id
//	@description				Application client id for the /api/auth/* endpoints.
//	@securityDefinitions.apikey	BearerAuth
//	@in							header
//	@name						Authorization
//	@description				"Bearer <JWT>" for end-user and admin callers.
//	@securityDefinitions.basic	BasicAuth
//	@description				HTTP Basic auth (client_id:client_secret) for the /oauth/* endpoints.
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// newRootCmd builds the `auth-service` root and attaches every subcommand.
// SilenceErrors/SilenceUsage keep runtime failures to a single "error: ..."
// line (printed by main) instead of cobra also dumping usage.
func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "auth-service",
		Short: "Go auth microservice: HTTP server plus seed/migrate maintenance commands",
		Long: "auth-service is the unified CLI for the Go auth module. The HTTP server and\n" +
			"every maintenance task is a subcommand of one binary; containers set the\n" +
			"entrypoint (e.g. `auth-service serve`).",
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	root.AddCommand(
		newServeCmd(),
		newSeedCmd(),
		newMigrateCmd(),
		newMigrateStorageCmd(),
	)
	return root
}
