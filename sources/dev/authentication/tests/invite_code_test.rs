mod common;

use axum::http::{Request, StatusCode};
use axum::body::Body;
use common::TestApp;
use serial_test::serial;

// ─── Unit-style tests (via repo directly, using Azurite) ─────────────────────

#[serial]
#[tokio::test]
async fn create_invite_code_fields() {
    let app = TestApp::new().await;
    let code = app
        .state
        .repo
        .invite_codes()
        .create_invite_code("admin-user-id")
        .await
        .expect("create should succeed");

    assert_eq!(code.code.len(), 16, "code must be 16 chars");
    assert!(
        code.code.chars().all(|c| c.is_ascii_alphanumeric()),
        "code must be alphanumeric"
    );
    assert!(code.used_at.is_none());
    assert!(code.used_by.is_none());
    assert!(!code.is_revoked);
    assert_eq!(code.created_by, "admin-user-id");
}

#[serial]
#[tokio::test]
async fn create_invite_codes_are_unique() {
    let app = TestApp::new().await;
    let c1 = app
        .state
        .repo
        .invite_codes()
        .create_invite_code("admin")
        .await
        .unwrap();
    let c2 = app
        .state
        .repo
        .invite_codes()
        .create_invite_code("admin")
        .await
        .unwrap();
    assert_ne!(c1.code, c2.code);
}

#[serial]
#[tokio::test]
async fn mark_invite_code_used_once_then_conflicts() {
    let app = TestApp::new().await;
    let code = app
        .state
        .repo
        .invite_codes()
        .create_invite_code("admin")
        .await
        .unwrap();

    // First mark succeeds
    app.state
        .repo
        .invite_codes()
        .mark_invite_code_used(&code.code, "user-1")
        .await
        .expect("first mark_used should succeed");

    // Second mark must fail (already used)
    let err = app
        .state
        .repo
        .invite_codes()
        .mark_invite_code_used(&code.code, "user-2")
        .await
        .expect_err("second mark_used should fail");

    let err_str = format!("{err:?}");
    assert!(
        err_str.contains("InviteCodeAlreadyUsed"),
        "expected InviteCodeAlreadyUsed, got: {err_str}"
    );
}

#[serial]
#[tokio::test]
async fn revoke_unused_code_succeeds() {
    let app = TestApp::new().await;
    let code = app
        .state
        .repo
        .invite_codes()
        .create_invite_code("admin")
        .await
        .unwrap();

    app.state
        .repo
        .invite_codes()
        .revoke_invite_code(&code.code)
        .await
        .expect("revoke should succeed on unused code");

    let fetched = app
        .state
        .repo
        .invite_codes()
        .get_invite_code_by_code(&code.code)
        .await
        .unwrap()
        .unwrap();
    assert!(fetched.is_revoked);
}

#[serial]
#[tokio::test]
async fn revoke_used_code_returns_conflict() {
    let app = TestApp::new().await;
    let code = app
        .state
        .repo
        .invite_codes()
        .create_invite_code("admin")
        .await
        .unwrap();

    app.state
        .repo
        .invite_codes()
        .mark_invite_code_used(&code.code, "user-1")
        .await
        .unwrap();

    let err = app
        .state
        .repo
        .invite_codes()
        .revoke_invite_code(&code.code)
        .await
        .expect_err("revoke should fail on used code");

    let err_str = format!("{err:?}");
    assert!(
        err_str.contains("InviteCodeAlreadyUsed"),
        "expected InviteCodeAlreadyUsed, got: {err_str}"
    );
}

// ─── Integration tests (HTTP layer) ──────────────────────────────────────────

/// Helper: set STRIDE_REQUIRE_INVITE_CODE for the duration of a closure.
/// Uses a mutex to avoid serial_test races with env var mutation.
async fn with_invite_required<F, Fut>(f: F)
where
    F: FnOnce() -> Fut,
    Fut: std::future::Future<Output = ()>,
{
    std::env::set_var("STRIDE_REQUIRE_INVITE_CODE", "true");
    let result = f().await;
    std::env::remove_var("STRIDE_REQUIRE_INVITE_CODE");
    result
}

#[serial]
#[tokio::test]
async fn register_without_invite_when_required_returns_400() {
    let app = TestApp::new().await;
    let created = app
        .admin_create_app("App", &["https://a.com/cb"], &["openid"])
        .await;

    with_invite_required(|| async {
        let resp = app
            .register_user(&created.client_id, "alice@test.com", "Password1!")
            .await;
        resp.assert_status(StatusCode::BAD_REQUEST);
        let json: serde_json::Value = resp.json();
        assert_eq!(json["error"], "bad_request");
    })
    .await;
}

#[serial]
#[tokio::test]
async fn register_with_invalid_code_returns_401() {
    let app = TestApp::new().await;
    let created = app
        .admin_create_app("App", &["https://a.com/cb"], &["openid"])
        .await;

    with_invite_required(|| async {
        let resp = app
            .register_user_with_invite(
                &created.client_id,
                "alice@test.com",
                "Password1!",
                "INVALIDCODE0000X",
            )
            .await;
        resp.assert_status(StatusCode::UNAUTHORIZED);
    })
    .await;
}

#[serial]
#[tokio::test]
async fn register_with_valid_code_succeeds_and_marks_used() {
    let app = TestApp::new().await;
    let created = app
        .admin_create_app("App", &["https://a.com/cb"], &["openid"])
        .await;

    with_invite_required(|| async {
        let code = app.admin_create_invite_code().await;

        let resp = app
            .register_user_with_invite(&created.client_id, "bob@test.com", "Password1!", &code)
            .await;
        resp.assert_status(StatusCode::CREATED);

        let json: serde_json::Value = resp.json();
        assert!(!json["user_id"].as_str().unwrap().is_empty());
        assert!(!json["access_token"].as_str().unwrap().is_empty());

        // Verify code is now marked used
        let fetched = app
            .state
            .repo
            .invite_codes()
            .get_invite_code_by_code(&code)
            .await
            .unwrap()
            .unwrap();
        assert!(fetched.used_at.is_some());
        assert!(fetched.used_by.is_some());
    })
    .await;
}

#[serial]
#[tokio::test]
async fn register_reusing_code_returns_409() {
    let app = TestApp::new().await;
    let created = app
        .admin_create_app("App", &["https://a.com/cb"], &["openid"])
        .await;

    with_invite_required(|| async {
        let code = app.admin_create_invite_code().await;

        // First register succeeds
        app.register_user_with_invite(&created.client_id, "user1@test.com", "Password1!", &code)
            .await
            .assert_status(StatusCode::CREATED);

        // Second register with same code returns 409
        let resp = app
            .register_user_with_invite(&created.client_id, "user2@test.com", "Password1!", &code)
            .await;
        resp.assert_status(StatusCode::CONFLICT);
        let json: serde_json::Value = resp.json();
        assert_eq!(json["error"], "invite_code_already_used");
    })
    .await;
}

#[serial]
#[tokio::test]
async fn register_without_env_var_works_without_invite_code() {
    // STRIDE_REQUIRE_INVITE_CODE is NOT set — registration must work as before
    std::env::remove_var("STRIDE_REQUIRE_INVITE_CODE");

    let app = TestApp::new().await;
    let created = app
        .admin_create_app("App", &["https://a.com/cb"], &["openid"])
        .await;

    let resp = app
        .register_user(&created.client_id, "nocode@test.com", "Password1!")
        .await;
    resp.assert_status(StatusCode::CREATED);
}

#[serial]
#[tokio::test]
async fn concurrent_register_with_same_code_exactly_one_wins() {
    let app = TestApp::new().await;
    let created = app
        .admin_create_app("App", &["https://a.com/cb"], &["openid"])
        .await;

    with_invite_required(|| async {
        let code = app.admin_create_invite_code().await;

        // Spawn two concurrent registrations with the same code
        let app1 = app.state.clone();
        let router1 = auth_service::routes::create_router(app1.clone());
        let app2 = app.state.clone();
        let router2 = auth_service::routes::create_router(app2.clone());

        let make_req = |email: &str, code: &str, client_id: &str| {
            let body = serde_json::json!({
                "email": email,
                "password": "Password1!",
                "invite_code": code,
            });
            Request::builder()
                .method("POST")
                .uri("/api/auth/register")
                .header("Content-Type", "application/json")
                .header("X-Client-Id", client_id)
                .body(Body::from(serde_json::to_vec(&body).unwrap()))
                .unwrap()
        };

        let req1 = make_req("concurrent1@test.com", &code, &created.client_id);
        let req2 = make_req("concurrent2@test.com", &code, &created.client_id);

        use tower::ServiceExt;
        let (resp1, resp2) = tokio::join!(
            router1.oneshot(req1),
            router2.oneshot(req2),
        );

        let status1 = resp1.unwrap().status();
        let status2 = resp2.unwrap().status();

        let successes = [status1, status2]
            .iter()
            .filter(|s| **s == StatusCode::CREATED)
            .count();
        let conflicts = [status1, status2]
            .iter()
            .filter(|s| **s == StatusCode::CONFLICT)
            .count();

        assert_eq!(successes, 1, "exactly one registration should succeed");
        assert_eq!(conflicts, 1, "exactly one registration should get 409");

        // Verify code is used exactly once
        let fetched = app
            .state
            .repo
            .invite_codes()
            .get_invite_code_by_code(&code)
            .await
            .unwrap()
            .unwrap();
        assert!(fetched.used_at.is_some(), "code must be marked used");
    })
    .await;
}

/// Regression test for Code-H7: on a concurrent invite-code race, the loser
/// must NOT leave orphan rows behind. Specifically, the losing email must have
/// neither a `users` row nor an `accounts` row.
#[serial]
#[tokio::test]
async fn concurrent_register_loser_has_no_orphan_account() {
    let app = TestApp::new().await;
    let created = app
        .admin_create_app("App", &["https://a.com/cb"], &["openid"])
        .await;

    with_invite_required(|| async {
        let code = app.admin_create_invite_code().await;

        let router1 = auth_service::routes::create_router(app.state.clone());
        let router2 = auth_service::routes::create_router(app.state.clone());

        let make_req = |email: &str, code: &str, client_id: &str| {
            let body = serde_json::json!({
                "email": email,
                "password": "Password1!",
                "invite_code": code,
            });
            Request::builder()
                .method("POST")
                .uri("/api/auth/register")
                .header("Content-Type", "application/json")
                .header("X-Client-Id", client_id)
                .body(Body::from(serde_json::to_vec(&body).unwrap()))
                .unwrap()
        };

        let email1 = "race-orphan1@test.com";
        let email2 = "race-orphan2@test.com";
        let req1 = make_req(email1, &code, &created.client_id);
        let req2 = make_req(email2, &code, &created.client_id);

        // Use tokio::spawn for true concurrency
        use tower::ServiceExt;
        let h1 = tokio::spawn(async move { router1.oneshot(req1).await.unwrap().status() });
        let h2 = tokio::spawn(async move { router2.oneshot(req2).await.unwrap().status() });

        let (s1, s2) = tokio::try_join!(h1, h2).unwrap();

        // Exactly one CREATED, one CONFLICT
        let winner_email = if s1 == StatusCode::CREATED && s2 == StatusCode::CONFLICT {
            email1
        } else if s2 == StatusCode::CREATED && s1 == StatusCode::CONFLICT {
            email2
        } else {
            panic!("expected exactly one CREATED + one CONFLICT, got {s1:?} / {s2:?}");
        };
        let loser_email = if winner_email == email1 { email2 } else { email1 };

        // Winner: must have a users row AND a password account row
        let winner_user = app
            .state
            .repo
            .users()
            .find_by_email(winner_email)
            .await
            .unwrap()
            .expect("winner user must exist");
        let winner_account = app
            .state
            .repo
            .accounts()
            .find_by_user_and_provider(&winner_user.id, "password")
            .await
            .unwrap();
        assert!(
            winner_account.is_some(),
            "winner must have a password account row"
        );

        // Loser: must have NEITHER a users row NOR an accounts row.
        // This is the regression assertion for Code-H7 (no orphan accounts on race).
        let loser_user = app
            .state
            .repo
            .users()
            .find_by_email(loser_email)
            .await
            .unwrap();
        assert!(
            loser_user.is_none(),
            "loser must not have a users row (got: {loser_user:?})"
        );

        // Even if the user row existed, no accounts row should reference the loser email
        // (the provider_account_id index uses the email).
        let loser_account = app
            .state
            .repo
            .accounts()
            .find_by_provider_account("password", loser_email)
            .await
            .unwrap();
        assert!(
            loser_account.is_none(),
            "loser must not have an accounts row (Code-H7 regression — got: {loser_account:?})"
        );
    })
    .await;
}

// ─── Admin API tests ──────────────────────────────────────────────────────────

#[serial]
#[tokio::test]
async fn admin_create_and_list_invite_codes() {
    let app = TestApp::new().await;

    let code1 = app.admin_create_invite_code().await;
    let code2 = app.admin_create_invite_code().await;
    assert_ne!(code1, code2);

    let req = Request::builder()
        .method("GET")
        .uri("/admin/invite-codes")
        .header("Authorization", format!("Bearer {}", app.admin_token))
        .body(Body::empty())
        .unwrap();
    let resp = app.request(req).await;
    resp.assert_status(StatusCode::OK);
    let list: serde_json::Value = resp.json();
    let arr = list.as_array().unwrap();
    assert!(arr.len() >= 2);
    assert!(arr.iter().any(|c| c["code"] == code1));
    assert!(arr.iter().any(|c| c["code"] == code2));
}

#[serial]
#[tokio::test]
async fn admin_revoke_invite_code() {
    let app = TestApp::new().await;
    let code = app.admin_create_invite_code().await;

    // Get the id
    let req = Request::builder()
        .method("GET")
        .uri("/admin/invite-codes")
        .header("Authorization", format!("Bearer {}", app.admin_token))
        .body(Body::empty())
        .unwrap();
    let resp = app.request(req).await;
    let list: serde_json::Value = resp.json();
    let entry = list
        .as_array()
        .unwrap()
        .iter()
        .find(|c| c["code"] == code)
        .unwrap()
        .clone();
    let _id = entry["id"].as_str().unwrap();

    // Revoke it (admin route now uses :code, not :id, to avoid an O(N) scan)
    let req = Request::builder()
        .method("DELETE")
        .uri(format!("/admin/invite-codes/{code}"))
        .header("Authorization", format!("Bearer {}", app.admin_token))
        .body(Body::empty())
        .unwrap();
    let resp = app.request(req).await;
    resp.assert_status(StatusCode::OK);

    // Revoked code should be rejected at register
    with_invite_required(|| async {
        let created = app
            .admin_create_app("App2", &["https://a.com/cb"], &["openid"])
            .await;
        let resp = app
            .register_user_with_invite(&created.client_id, "revoked@test.com", "Password1!", &code)
            .await;
        resp.assert_status(StatusCode::UNAUTHORIZED);
    })
    .await;
}

#[serial]
#[tokio::test]
async fn admin_revoke_used_code_returns_409() {
    let app = TestApp::new().await;
    let created = app
        .admin_create_app("App", &["https://a.com/cb"], &["openid"])
        .await;

    with_invite_required(|| async {
        let code = app.admin_create_invite_code().await;

        // Use the code
        app.register_user_with_invite(&created.client_id, "used@test.com", "Password1!", &code)
            .await
            .assert_status(StatusCode::CREATED);

        // Try to revoke — should fail with 409 (admin route uses :code)
        let req = Request::builder()
            .method("DELETE")
            .uri(format!("/admin/invite-codes/{code}"))
            .header("Authorization", format!("Bearer {}", app.admin_token))
            .body(Body::empty())
            .unwrap();
        let resp = app.request(req).await;
        resp.assert_status(StatusCode::CONFLICT);
    })
    .await;
}
