use async_trait::async_trait;
use serde::{Deserialize, Serialize};

use super::{AuthProvider, ProviderUserInfo};
use crate::error::AppError;

#[derive(Debug, Clone)]
pub struct WeChatProvider {
    appid: String,
    secret: String,
    http_client: reqwest::Client,
}

#[derive(Debug, Deserialize)]
struct WeChatConfig {
    appid: String,
    secret: String,
}

#[derive(Debug, Deserialize)]
pub struct WeChatCredential {
    pub code: String,
}

#[derive(Debug, Deserialize, Serialize)]
struct JsCode2SessionResponse {
    openid: Option<String>,
    session_key: Option<String>,
    unionid: Option<String>,
    errcode: Option<i64>,
    errmsg: Option<String>,
}

impl WeChatProvider {
    pub fn from_config(config: &serde_json::Value) -> Result<Self, AppError> {
        let wechat_config: WeChatConfig = serde_json::from_value(config.clone())
            .map_err(|e| AppError::BadRequest(format!("Invalid WeChat config: {e}")))?;

        Ok(Self {
            appid: wechat_config.appid,
            secret: wechat_config.secret,
            http_client: reqwest::Client::new(),
        })
    }
}

#[async_trait]
impl AuthProvider for WeChatProvider {
    fn provider_id(&self) -> &str {
        "wechat"
    }

    async fn authenticate(
        &self,
        credential: &serde_json::Value,
    ) -> Result<ProviderUserInfo, AppError> {
        let cred: WeChatCredential = serde_json::from_value(credential.clone()).map_err(|_| {
            AppError::BadRequest(
                "Invalid WeChat credential: expected {\"code\": \"...\"}".to_string(),
            )
        })?;

        // Build URL with properly encoded query parameters
        let url = reqwest::Url::parse_with_params(
            "https://api.weixin.qq.com/sns/jscode2session",
            &[
                ("appid", self.appid.as_str()),
                ("secret", self.secret.as_str()),
                ("js_code", cred.code.as_str()),
                ("grant_type", "authorization_code"),
            ],
        )
        .map_err(|e| AppError::Internal(format!("Failed to build WeChat URL: {e}")))?;

        let resp: JsCode2SessionResponse = self.http_client.get(url).send().await?.json().await?;

        // Check for errors
        if let Some(errcode) = resp.errcode {
            if errcode != 0 {
                let errmsg = resp.errmsg.unwrap_or_default();
                return Err(AppError::BadRequest(format!(
                    "WeChat API error {errcode}: {errmsg}"
                )));
            }
        }

        let openid = resp
            .openid
            .ok_or_else(|| AppError::BadRequest("WeChat API did not return openid".to_string()))?;

        // Do NOT persist session_key — it is a sensitive server-side secret
        // used to decrypt user data from the WeChat client.
        let metadata = serde_json::json!({
            "openid": openid,
            "unionid": resp.unionid,
        });

        Ok(ProviderUserInfo {
            provider_account_id: openid,
            email: None,
            name: None,
            avatar_url: None,
            metadata,
        })
    }
}
