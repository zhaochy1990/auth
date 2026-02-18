use std::time::Duration;

use axum::{
    middleware,
    routing::{delete, get, patch, post},
    Router,
};
use tower_http::cors::{Any, CorsLayer};
use tower_http::trace::TraceLayer;

use crate::handlers;
use crate::rate_limit::{rate_limit_middleware, RateLimiter};
use crate::AppState;

pub fn create_router(state: AppState) -> Router {
    let cors = CorsLayer::new()
        .allow_origin(Any)
        .allow_methods(Any)
        .allow_headers(Any);

    // Rate limiters: per-IP sliding window
    // Auth: 20 requests per 60 seconds (login/register brute-force protection)
    let auth_limiter = RateLimiter::new(20, Duration::from_secs(60));
    // OAuth2: 30 requests per 60 seconds
    let oauth_limiter = RateLimiter::new(30, Duration::from_secs(60));

    // OAuth2 endpoints (client authenticates with Basic auth)
    let oauth2_routes = Router::new()
        .route("/token", post(handlers::oauth2::token))
        .route("/revoke", post(handlers::oauth2::revoke))
        .route("/introspect", post(handlers::oauth2::introspect))
        .route_layer(middleware::from_fn_with_state(
            oauth_limiter,
            rate_limit_middleware,
        ));

    // Auth endpoints (user-facing, require X-Client-Id) — rate limited
    let auth_routes = Router::new()
        .route("/register", post(handlers::auth::register))
        .route("/login", post(handlers::auth::login))
        .route(
            "/provider/:provider_id/login",
            post(handlers::auth::provider_login),
        )
        .route("/refresh", post(handlers::auth::refresh))
        .route("/logout", post(handlers::auth::logout))
        .route_layer(middleware::from_fn_with_state(
            auth_limiter,
            rate_limit_middleware,
        ));

    // User endpoints (require Bearer token)
    let user_routes = Router::new()
        .route("/me", get(handlers::user::get_profile))
        .route("/me", patch(handlers::user::update_profile))
        .route("/me/accounts", get(handlers::user::list_accounts))
        .route(
            "/me/accounts/:provider_id/link",
            post(handlers::user::link_account),
        )
        .route(
            "/me/accounts/:provider_id",
            delete(handlers::user::unlink_account),
        );

    // Admin endpoints (require X-Admin-Key or Bearer with admin role)
    let admin_routes = Router::new()
        .route("/applications", post(handlers::admin::create_application))
        .route("/applications", get(handlers::admin::list_applications))
        .route(
            "/applications/:id",
            patch(handlers::admin::update_application),
        )
        .route(
            "/applications/:id/providers",
            get(handlers::admin::list_providers).post(handlers::admin::add_provider),
        )
        .route(
            "/applications/:id/providers/:provider_id",
            delete(handlers::admin::remove_provider),
        )
        .route(
            "/applications/:id/rotate-secret",
            post(handlers::admin::rotate_secret),
        )
        .route("/users", get(handlers::admin::list_users))
        .route("/users/:id", get(handlers::admin::get_user).patch(handlers::admin::update_user))
        .route(
            "/users/:id/accounts",
            get(handlers::admin::get_user_accounts),
        )
        .route(
            "/users/:id/accounts/:provider_id",
            delete(handlers::admin::admin_unlink_account),
        )
        .route("/stats", get(handlers::admin::stats));

    Router::new()
        .nest("/oauth", oauth2_routes)
        .nest("/api/auth", auth_routes)
        .nest("/api/users", user_routes)
        .nest("/admin", admin_routes)
        .route("/health", get(health_check))
        .layer(TraceLayer::new_for_http())
        .layer(cors)
        .with_state(state)
}

async fn health_check() -> &'static str {
    "ok"
}
