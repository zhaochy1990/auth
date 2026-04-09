use async_trait::async_trait;
use chrono::NaiveDateTime;

use crate::db::models::{Account, AppProvider, Application, AuthorizationCode, RefreshToken, User};
use crate::error::AppError;

#[async_trait]
pub trait UserRepository: Send + Sync {
    async fn find_by_id(&self, id: &str) -> Result<Option<User>, AppError>;
    async fn find_by_email(&self, email: &str) -> Result<Option<User>, AppError>;
    async fn insert(&self, user: &User) -> Result<(), AppError>;
    async fn update(&self, user: &User) -> Result<(), AppError>;
    async fn count_all(&self) -> Result<u64, AppError>;
    async fn count_since(&self, since: NaiveDateTime) -> Result<u64, AppError>;
    async fn list_paginated(
        &self,
        search: Option<&str>,
        offset: u64,
        limit: u64,
    ) -> Result<(Vec<User>, u64), AppError>;
}

#[async_trait]
pub trait ApplicationRepository: Send + Sync {
    async fn find_by_id(&self, id: &str) -> Result<Option<Application>, AppError>;
    async fn find_by_client_id(&self, client_id: &str) -> Result<Option<Application>, AppError>;
    async fn find_by_name(&self, name: &str) -> Result<Option<Application>, AppError>;
    async fn find_all(&self) -> Result<Vec<Application>, AppError>;
    async fn insert(&self, app: &Application) -> Result<(), AppError>;
    async fn update(&self, app: &Application) -> Result<(), AppError>;
    async fn count_all(&self) -> Result<u64, AppError>;
    async fn count_active(&self) -> Result<u64, AppError>;
}

#[async_trait]
pub trait AccountRepository: Send + Sync {
    async fn find_by_user_and_provider(
        &self,
        user_id: &str,
        provider_id: &str,
    ) -> Result<Option<Account>, AppError>;
    async fn find_by_provider_account(
        &self,
        provider_id: &str,
        provider_account_id: &str,
    ) -> Result<Option<Account>, AppError>;
    async fn find_all_by_user(&self, user_id: &str) -> Result<Vec<Account>, AppError>;
    async fn count_by_user(&self, user_id: &str) -> Result<u64, AppError>;
    async fn insert(&self, account: &Account) -> Result<(), AppError>;
    async fn update(&self, account: &Account) -> Result<(), AppError>;
    async fn delete_by_id(&self, id: &str) -> Result<(), AppError>;
}

#[async_trait]
pub trait AppProviderRepository: Send + Sync {
    async fn find_by_app_and_provider(
        &self,
        app_id: &str,
        provider_id: &str,
    ) -> Result<Option<AppProvider>, AppError>;
    async fn find_all_by_app(&self, app_id: &str) -> Result<Vec<AppProvider>, AppError>;
    async fn insert(&self, ap: &AppProvider) -> Result<(), AppError>;
    async fn delete_by_id(&self, id: &str) -> Result<(), AppError>;
}

#[async_trait]
pub trait AuthCodeRepository: Send + Sync {
    async fn find_by_code(&self, code: &str) -> Result<Option<AuthorizationCode>, AppError>;
    async fn insert(&self, code: &AuthorizationCode) -> Result<(), AppError>;
    async fn mark_used(&self, code: &str) -> Result<(), AppError>;
}

#[async_trait]
pub trait RefreshTokenRepository: Send + Sync {
    async fn find_by_token_hash(&self, hash: &str) -> Result<Option<RefreshToken>, AppError>;
    async fn insert(&self, token: &RefreshToken) -> Result<(), AppError>;
    async fn revoke(&self, id: &str) -> Result<(), AppError>;
}

pub trait Repository: Send + Sync {
    fn users(&self) -> &dyn UserRepository;
    fn applications(&self) -> &dyn ApplicationRepository;
    fn accounts(&self) -> &dyn AccountRepository;
    fn app_providers(&self) -> &dyn AppProviderRepository;
    fn auth_codes(&self) -> &dyn AuthCodeRepository;
    fn refresh_tokens(&self) -> &dyn RefreshTokenRepository;
}
