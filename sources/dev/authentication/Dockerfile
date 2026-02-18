# === Builder stage ===
FROM rust:1.85-slim-bookworm AS builder

RUN apt-get update && apt-get install -y pkg-config libsqlite3-dev && rm -rf /var/lib/apt/lists/*

WORKDIR /app

# Copy workspace manifests first for better layer caching
COPY Cargo.toml Cargo.lock ./
COPY entity/Cargo.toml entity/Cargo.toml
COPY migration/Cargo.toml migration/Cargo.toml

# Create dummy sources to cache dependency builds
RUN mkdir -p src entity/src migration/src \
    && echo "fn main() {}" > src/main.rs \
    && touch src/lib.rs entity/src/lib.rs migration/src/lib.rs \
    && cargo build --release 2>/dev/null || true \
    && rm -rf src entity/src migration/src

# Copy real source code
COPY src/ src/
COPY entity/ entity/
COPY migration/ migration/

# Build the release binary
RUN cargo build --release

# === Runtime stage ===
FROM debian:bookworm-slim

WORKDIR /app

COPY --from=builder /app/target/release/auth-service ./

# Bind to all interfaces so the container is reachable
ENV SERVER_HOST=0.0.0.0

EXPOSE 3000

CMD ["./auth-service"]
