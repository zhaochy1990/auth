# syntax=docker/dockerfile:1
#
# Self-contained build using vendored dependencies. Run `go mod vendor` in this
# directory before building so the local `replace`d `x` module (and the swaggo
# packages) are captured in vendor/ (the build does not reach outside the build
# context). The committed cmd/auth-service/docs package is compiled in via
# `-tags swagger`, so the /swagger UI is available when SWAGGER_ENABLED=true.
#
#   go mod vendor
#   docker build -t auth-service-go .
#
# The image bundles every subcommand in one binary (cobra): the default command
# is `serve`; override it to run maintenance tasks, e.g.
#   docker run auth-service-go seed admin@example.com

FROM golang:1.25-bookworm AS build
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -mod=vendor -tags swagger -trimpath -o /out/auth-service ./cmd/auth-service

FROM gcr.io/distroless/static-debian12
WORKDIR /app
COPY --from=build /out/auth-service /app/auth-service
ARG APP_VERSION=dev
ENV APP_VERSION=$APP_VERSION
# JWT keys are NOT baked into the image — mount them at /app/keys at runtime
# (docker-compose volume locally; Azure File Share in production).
EXPOSE 3000
ENTRYPOINT ["/app/auth-service"]
CMD ["serve"]
