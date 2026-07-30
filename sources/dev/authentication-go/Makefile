# Makefile for the Go auth microservice (authentication-go). One `auth-service`
# binary bundles the HTTP server and every maintenance task as a cobra
# subcommand (`auth-service serve`, `auth-service seed`, `auth-service migrate`,
# `auth-service migrate-storage`); containers just set the entrypoint.

SWAG_VERSION := v1.16.6

.PHONY: build build-service build-service-swagger swagger test vet fmt fmt-check vendor

# Compile-check every package (no output binary emitted).
build:
	go build ./...

# Build the single binary to bin/auth-service (WITHOUT the Swagger UI). Use
# `make build-service-swagger` for the Swagger-enabled build.
build-service:
	go build -o bin/auth-service ./cmd/auth-service

# Generate the OpenAPI/Swagger docs (cmd/auth-service/docs). Committed and
# regenerated here; the general API info lives on cmd/auth-service/main.go.
swagger:
	go run github.com/swaggo/swag/cmd/swag@$(SWAG_VERSION) init -g cmd/auth-service/main.go -o cmd/auth-service/docs --parseInternal --parseDependency

# Build the binary with the Swagger UI compiled in (requires `make swagger`).
# The UI is served by `auth-service serve` when SWAGGER_ENABLED=true.
build-service-swagger: swagger
	go build -tags swagger -o bin/auth-service ./cmd/auth-service

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

fmt-check:
	gofmt -l .

# Capture the local `replace`d `x` module into vendor/ before docker build.
vendor:
	go mod vendor
