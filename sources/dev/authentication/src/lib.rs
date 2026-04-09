pub mod auth;
pub mod config;
pub mod db;
pub mod error;
pub mod handlers;
pub mod rate_limit;
pub mod routes;
pub mod seed;

use std::sync::Arc;

use config::Config;
use db::repository::Repository;

#[derive(Clone)]
pub struct AppState {
    pub repo: Arc<dyn Repository>,
    pub jwt: auth::jwt::JwtManager,
    pub config: Config,
}

impl AsRef<AppState> for AppState {
    fn as_ref(&self) -> &AppState {
        self
    }
}
