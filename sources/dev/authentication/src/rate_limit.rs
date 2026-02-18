use std::collections::HashMap;
use std::sync::Arc;
use std::time::{Duration, Instant};

use axum::{
    body::Body,
    extract::State,
    http::{Request, StatusCode},
    middleware::Next,
    response::{IntoResponse, Response},
};
use tokio::sync::Mutex;

/// Per-key sliding window rate limiter.
#[derive(Clone)]
pub struct RateLimiter {
    state: Arc<Mutex<RateLimiterInner>>,
    max_requests: u32,
    window: Duration,
}

struct RateLimiterInner {
    buckets: HashMap<String, Vec<Instant>>,
    last_cleanup: Instant,
}

impl RateLimiter {
    pub fn new(max_requests: u32, window: Duration) -> Self {
        Self {
            state: Arc::new(Mutex::new(RateLimiterInner {
                buckets: HashMap::new(),
                last_cleanup: Instant::now(),
            })),
            max_requests,
            window,
        }
    }

    async fn check(&self, key: &str) -> bool {
        let mut inner = self.state.lock().await;
        let now = Instant::now();

        // Periodic cleanup of expired entries (every 60s)
        if now.duration_since(inner.last_cleanup) > Duration::from_secs(60) {
            inner.buckets.retain(|_, timestamps| {
                timestamps
                    .last()
                    .is_some_and(|t| now.duration_since(*t) < self.window)
            });
            inner.last_cleanup = now;
        }

        let timestamps = inner.buckets.entry(key.to_string()).or_default();

        // Remove expired timestamps
        timestamps.retain(|t| now.duration_since(*t) < self.window);

        if timestamps.len() >= self.max_requests as usize {
            return false;
        }

        timestamps.push(now);
        true
    }
}

/// Axum middleware that rate-limits by client IP (from X-Forwarded-For or
/// ConnectInfo, falling back to a global bucket).
pub async fn rate_limit_middleware(
    State(limiter): State<RateLimiter>,
    req: Request<Body>,
    next: Next,
) -> Response {
    // Extract IP: try X-Forwarded-For, then X-Real-IP, fallback to "global"
    let key = req
        .headers()
        .get("x-forwarded-for")
        .and_then(|v| v.to_str().ok())
        .and_then(|v| v.split(',').next())
        .map(|s| s.trim().to_string())
        .or_else(|| {
            req.headers()
                .get("x-real-ip")
                .and_then(|v| v.to_str().ok())
                .map(|s| s.to_string())
        })
        .unwrap_or_else(|| "global".to_string());

    if !limiter.check(&key).await {
        return (
            StatusCode::TOO_MANY_REQUESTS,
            axum::Json(serde_json::json!({
                "error": "rate_limited",
                "message": "Too many requests. Please try again later."
            })),
        )
            .into_response();
    }

    next.run(req).await
}

#[cfg(test)]
mod tests {
    use super::*;

    #[tokio::test]
    async fn allows_requests_under_limit() {
        let limiter = RateLimiter::new(3, Duration::from_secs(60));
        assert!(limiter.check("ip1").await);
        assert!(limiter.check("ip1").await);
        assert!(limiter.check("ip1").await);
    }

    #[tokio::test]
    async fn blocks_requests_over_limit() {
        let limiter = RateLimiter::new(2, Duration::from_secs(60));
        assert!(limiter.check("ip1").await);
        assert!(limiter.check("ip1").await);
        assert!(!limiter.check("ip1").await);
    }

    #[tokio::test]
    async fn separate_keys_have_separate_limits() {
        let limiter = RateLimiter::new(1, Duration::from_secs(60));
        assert!(limiter.check("ip1").await);
        assert!(limiter.check("ip2").await);
        assert!(!limiter.check("ip1").await);
        assert!(!limiter.check("ip2").await);
    }

    #[tokio::test]
    async fn window_expiry_resets_count() {
        let limiter = RateLimiter::new(1, Duration::from_millis(50));
        assert!(limiter.check("ip1").await);
        assert!(!limiter.check("ip1").await);
        tokio::time::sleep(Duration::from_millis(60)).await;
        assert!(limiter.check("ip1").await);
    }

    #[tokio::test]
    async fn cleanup_removes_expired_buckets() {
        let limiter = RateLimiter::new(10, Duration::from_millis(10));
        limiter.check("expired-key").await;

        tokio::time::sleep(Duration::from_millis(20)).await;

        // Force cleanup by setting last_cleanup far in the past
        {
            let mut inner = limiter.state.lock().await;
            inner.last_cleanup = Instant::now() - Duration::from_secs(120);
        }

        // Trigger cleanup via a check
        limiter.check("new-key").await;

        let inner = limiter.state.lock().await;
        assert!(!inner.buckets.contains_key("expired-key"));
    }
}
