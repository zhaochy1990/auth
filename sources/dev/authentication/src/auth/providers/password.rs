use async_trait::async_trait;
use serde::Deserialize;

use super::{AuthProvider, ProviderUserInfo};
use crate::error::AppError;

/// Password-based authentication provider.
/// This is used internally — it doesn't implement the `AuthProvider` trait
/// in the same way as external providers, since password auth is handled
/// directly in the auth handlers (register/login). This module provides
/// helper types for password credential validation.

#[derive(Debug, Deserialize)]
pub struct PasswordCredential {
    pub email: String,
    pub password: String,
}

pub struct PasswordProvider;

#[async_trait]
impl AuthProvider for PasswordProvider {
    fn provider_id(&self) -> &str {
        "password"
    }

    async fn authenticate(
        &self,
        credential: &serde_json::Value,
    ) -> Result<ProviderUserInfo, AppError> {
        let cred: PasswordCredential = serde_json::from_value(credential.clone())
            .map_err(|_| AppError::BadRequest("Invalid password credential format".to_string()))?;

        // For password auth, the provider_account_id is the email.
        // Actual password verification is done in the auth handler.
        Ok(ProviderUserInfo {
            provider_account_id: cred.email.clone(),
            email: Some(cred.email),
            name: None,
            avatar_url: None,
            metadata: serde_json::json!({}),
        })
    }
}
