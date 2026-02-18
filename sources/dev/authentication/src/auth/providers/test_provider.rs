use async_trait::async_trait;
use serde::Deserialize;

use super::{AuthProvider, ProviderUserInfo};
use crate::error::AppError;

#[derive(Debug, Clone)]
pub struct TestProvider;

#[derive(Debug, Deserialize)]
struct TestCredential {
    account_id: String,
    email: Option<String>,
    name: Option<String>,
}

impl TestProvider {
    pub fn new(_config: &serde_json::Value) -> Self {
        Self
    }
}

#[async_trait]
impl AuthProvider for TestProvider {
    fn provider_id(&self) -> &str {
        "test"
    }

    async fn authenticate(
        &self,
        credential: &serde_json::Value,
    ) -> Result<ProviderUserInfo, AppError> {
        let cred: TestCredential = serde_json::from_value(credential.clone())
            .map_err(|_| AppError::BadRequest("Invalid test credential".to_string()))?;

        Ok(ProviderUserInfo {
            provider_account_id: cred.account_id,
            email: cred.email,
            name: cred.name,
            avatar_url: None,
            metadata: serde_json::json!({"provider": "test"}),
        })
    }
}
