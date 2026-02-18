# === Builder stage ===
FROM rust:1.85-slim-bookworm AS builder

WORKDIR /app

# Copy manifest first for better layer caching
COPY Cargo.toml Cargo.lock ./

# Create dummy source to cache dependency builds
RUN mkdir -p src \
    && echo "fn main() {}" > src/main.rs \
    && touch src/lib.rs \
    && cargo build --release 2>/dev/null || true \
    && rm -rf src

# Copy real source code
COPY src/ src/
COPY sql/ sql/

# Build the release binary
RUN cargo build --release

# === Runtime stage ===
FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y ca-certificates && rm -rf /var/lib/apt/lists/*

WORKDIR /app

COPY --from=builder /app/target/release/auth-service ./

# Bind to all interfaces so the container is reachable
ENV SERVER_HOST=0.0.0.0

EXPOSE 3000

CMD ["./auth-service"]
