//go:build !swagger

package server

import "github.com/gin-gonic/gin"

// mountSwagger is a no-op in the default build. The Swagger UI + generated docs
// package are compiled in only with `-tags swagger` (after `swag init` has run),
// so plain `go build`/`go test` never need the generated code.
func mountSwagger(_ *gin.Engine, _ bool) {}
