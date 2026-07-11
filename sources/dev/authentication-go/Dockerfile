# syntax=docker/dockerfile:1
#
# Self-contained build using vendored dependencies. Run `go mod vendor` in this
# directory before building so the local `replace`d `x` module is captured in
# vendor/ (the build does not reach outside the build context).
#
#   go mod vendor
#   docker build -t auth-service-go .

FROM golang:1.25-bookworm AS build
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -mod=vendor -trimpath -o /out/auth-service ./cmd/auth-service

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /out/auth-service /app/auth-service
ARG APP_VERSION=dev
ENV APP_VERSION=$APP_VERSION
# JWT keys are NOT baked into the image — mount them at /app/keys at runtime
# (docker-compose volume locally; Azure File Share in production).
EXPOSE 3000
ENTRYPOINT ["/app/auth-service"]
