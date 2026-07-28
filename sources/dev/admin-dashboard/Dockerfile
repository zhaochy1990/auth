# syntax=docker/dockerfile:1
#
# Admin dashboard (Vite/React SPA) packaged as a container and served by nginx,
# replacing the previous "rsync dist/ to the server + Caddy file_server" flow.
# Built same-origin (VITE_API_BASE_URL="") so it calls the auth API via relative
# paths behind the front Caddy, which reverse-proxies to this container.
#
#   docker build \
#     --build-arg VITE_API_CLIENT_ID=... \
#     --build-arg VITE_APP_VERSION=... \
#     -t auth-frontend .

# --- build the static bundle ---
FROM node:22-alpine AS build
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci
COPY . .
# Vite inlines VITE_* at build time, so they must be present before `npm run build`.
ARG VITE_API_CLIENT_ID
ARG VITE_API_BASE_URL=""
ARG VITE_APP_VERSION=dev
ENV VITE_API_CLIENT_ID=$VITE_API_CLIENT_ID \
    VITE_API_BASE_URL=$VITE_API_BASE_URL \
    VITE_APP_VERSION=$VITE_APP_VERSION
RUN npm run build

# --- serve with nginx ---
FROM nginx:1.27-alpine
COPY nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build /app/dist /usr/share/nginx/html
EXPOSE 80
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
  CMD wget -qO- http://127.0.0.1/ >/dev/null 2>&1 || exit 1
