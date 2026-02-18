pub mod auth;
pub mod config;
pub mod db;
pub mod error;
pub mod handlers;
pub mod rate_limit;
pub mod routes;
pub mod seed;

use db::pool::Db;

use config::Config;

#[derive(Clone)]
pub struct AppState {
    pub db: Db,
    pub jwt: auth::jwt::JwtManager,
    pub config: Config,
}

impl AsRef<AppState> for AppState {
    fn as_ref(&self) -> &AppState {
        self
    }
}
