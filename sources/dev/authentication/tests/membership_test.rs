mod common;

use auth_service::db::models::MembershipTier;
use axum::body::Body;
use axum::http::{Request, StatusCode};
use common::{TestApp, TestResponse};
use serial_test::serial;

fn access_token(resp: &TestResponse) -> String {
    let body: serde_json::Value = resp.json();
    body["access_token"]
        .as_str()
        .expect("response should contain access_token")
        .to_string()
}

async fn patch_user(app: &TestApp, user_id: &str, body: serde_json::Value) -> TestResponse {
    let req = Request::builder()
        .method("PATCH")
        .uri(format!("/admin/users/{user_id}"))
        .header("Content-Type", "application/json")
        .header("Authorization", format!("Bearer {}", app.admin_token))
        .body(Body::from(serde_json::to_vec(&body).unwrap()))
        .unwrap();
    app.request(req).await
}

#[serial]
#[tokio::test]
async fn register_defaults_to_regular_membership() {
    let app = TestApp::new().await;
    let created = app
        .admin_create_app("App", &["https://a.com/cb"], &["openid"])
        .await;

    let resp = app
        .register_user(&created.client_id, "reg@test.com", "Password1!")
        .await;
    resp.assert_status(StatusCode::CREATED);
    let token = access_token(&resp);

    // Stored tier defaults to regular, with no expiry.
    let user = app
        .state
        .repo
        .users()
        .find_by_email("reg@test.com")
        .await
        .unwrap()
        .expect("user must exist");
    assert_eq!(user.membership, MembershipTier::Regular);
    assert!(user.membership_expires_at.is_none());

    // The access token embeds the regular tier.
    let claims = app.state.jwt.verify_access_token(&token).unwrap();
    assert_eq!(claims.membership, MembershipTier::Regular);
}

#[serial]
#[tokio::test]
async fn admin_can_set_and_clear_membership() {
    let app = TestApp::new().await;
    let created = app
        .admin_create_app("App", &["https://a.com/cb"], &["openid"])
        .await;
    app.register_user(&created.client_id, "m@test.com", "Password1!")
        .await
        .assert_status(StatusCode::CREATED);
    let user = app
        .state
        .repo
        .users()
        .find_by_email("m@test.com")
        .await
        .unwrap()
        .unwrap();

    // Upgrade to vip1 with an explicit expiry.
    let resp = patch_user(
        &app,
        &user.id,
        serde_json::json!({"membership": "vip1", "membership_expires_at": "2999-01-01"}),
    )
    .await;
    resp.assert_status(StatusCode::OK);
    let body: serde_json::Value = resp.json();
    assert_eq!(body["membership"], "vip1");
    assert!(
        body["membership_expires_at"].as_str().is_some(),
        "expiry should be set, got {body}"
    );

    // Downgrading to regular clears the expiry.
    let resp = patch_user(&app, &user.id, serde_json::json!({"membership": "regular"})).await;
    resp.assert_status(StatusCode::OK);
    let body: serde_json::Value = resp.json();
    assert_eq!(body["membership"], "regular");
    assert!(
        body["membership_expires_at"].is_null(),
        "expiry should be cleared, got {body}"
    );
}

#[serial]
#[tokio::test]
async fn invite_code_grants_membership_on_register() {
    let app = TestApp::new().await;
    let created = app
        .admin_create_app("App", &["https://a.com/cb"], &["openid"])
        .await;

    // A long-term code that grants vip1 for 30 days.
    let req = Request::builder()
        .method("POST")
        .uri("/admin/invite-codes?kind=long_term&grants_membership=vip1&grants_membership_days=30")
        .header("Authorization", format!("Bearer {}", app.admin_token))
        .body(Body::empty())
        .unwrap();
    let resp = app.request(req).await;
    resp.assert_status(StatusCode::OK);
    let body: serde_json::Value = resp.json();
    assert_eq!(body["grants_membership"], "vip1");
    assert_eq!(body["grants_membership_days"], 30);
    let code = body["code"].as_str().unwrap().to_string();

    // Invite codes are only consulted when registration is invite-gated.
    std::env::set_var("STRIDE_REQUIRE_INVITE_CODE", "true");
    let resp = app
        .register_user_with_invite(&created.client_id, "g@test.com", "Password1!", &code)
        .await;
    std::env::remove_var("STRIDE_REQUIRE_INVITE_CODE");
    resp.assert_status(StatusCode::CREATED);
    let token = access_token(&resp);

    let user = app
        .state
        .repo
        .users()
        .find_by_email("g@test.com")
        .await
        .unwrap()
        .unwrap();
    assert_eq!(user.membership, MembershipTier::Vip1);
    assert!(
        user.membership_expires_at.is_some(),
        "a 30-day grant should set an expiry"
    );

    let claims = app.state.jwt.verify_access_token(&token).unwrap();
    assert_eq!(claims.membership, MembershipTier::Vip1);
}

#[serial]
#[tokio::test]
async fn expired_membership_downgrades_on_login() {
    let app = TestApp::new().await;
    let created = app
        .admin_create_app("App", &["https://a.com/cb"], &["openid"])
        .await;
    app.register_user(&created.client_id, "exp@test.com", "Password1!")
        .await
        .assert_status(StatusCode::CREATED);

    // Force a vip1 membership that already expired in the past.
    let mut user = app
        .state
        .repo
        .users()
        .find_by_email("exp@test.com")
        .await
        .unwrap()
        .unwrap();
    user.membership = MembershipTier::Vip1;
    user.membership_expires_at = Some(
        chrono::NaiveDate::from_ymd_opt(2000, 1, 1)
            .unwrap()
            .and_hms_opt(0, 0, 0)
            .unwrap(),
    );
    app.state.repo.users().update(&user).await.unwrap();

    // Logging in resolves the effective tier and persists the downgrade.
    let resp = app
        .login_user(&created.client_id, "exp@test.com", "Password1!")
        .await;
    resp.assert_status(StatusCode::OK);
    let token = access_token(&resp);

    let claims = app.state.jwt.verify_access_token(&token).unwrap();
    assert_eq!(claims.membership, MembershipTier::Regular);

    let user = app
        .state
        .repo
        .users()
        .find_by_email("exp@test.com")
        .await
        .unwrap()
        .unwrap();
    assert_eq!(user.membership, MembershipTier::Regular);
    assert!(user.membership_expires_at.is_none());
}

#[serial]
#[tokio::test]
async fn invalid_membership_value_is_rejected() {
    let app = TestApp::new().await;
    let created = app
        .admin_create_app("App", &["https://a.com/cb"], &["openid"])
        .await;
    app.register_user(&created.client_id, "bad@test.com", "Password1!")
        .await
        .assert_status(StatusCode::CREATED);
    let user = app
        .state
        .repo
        .users()
        .find_by_email("bad@test.com")
        .await
        .unwrap()
        .unwrap();

    let resp = patch_user(&app, &user.id, serde_json::json!({"membership": "vip9"})).await;
    assert!(
        resp.status.is_client_error(),
        "an unknown tier must be rejected, got {}",
        resp.status
    );
}
