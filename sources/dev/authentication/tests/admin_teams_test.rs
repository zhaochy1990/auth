// Requires Azurite running on port 10002 (cargo test will fail otherwise).

mod common;

use axum::body::Body;
use axum::http::{Request, StatusCode};
use common::TestApp;
use serial_test::serial;

/// Register a new user via /api/auth/register and return (access_token, user_id).
async fn setup_user(app: &TestApp, email: &str) -> (String, String) {
    let created = app
        .admin_create_app(
            &format!("AdminTeamsApp {email}"),
            &["https://example.com/cb"],
            &["openid", "profile"],
        )
        .await;
    let resp = app
        .register_user(&created.client_id, email, "Password1!")
        .await;
    resp.assert_status(StatusCode::CREATED);
    let json: serde_json::Value = resp.json();
    (
        json["access_token"].as_str().unwrap().to_string(),
        json["user_id"].as_str().unwrap().to_string(),
    )
}

async fn admin_create_team(app: &TestApp, name: &str, owner_user_id: &str) -> serde_json::Value {
    let body = serde_json::json!({
        "name": name,
        "description": "test description",
        "owner_user_id": owner_user_id,
    });
    let req = Request::builder()
        .method("POST")
        .uri("/admin/teams")
        .header("Content-Type", "application/json")
        .header("Authorization", format!("Bearer {}", app.admin_token))
        .body(Body::from(serde_json::to_vec(&body).unwrap()))
        .unwrap();
    let resp = app.request(req).await;
    resp.assert_status(StatusCode::OK);
    resp.json()
}

#[serial]
#[tokio::test]
async fn admin_create_team_for_arbitrary_owner() {
    let app = TestApp::new().await;
    let (_token, owner_id) = setup_user(&app, "ownertarget@test.com").await;

    let team = admin_create_team(&app, "Admin-Made Team", &owner_id).await;
    assert_eq!(team["name"], "Admin-Made Team");
    assert_eq!(team["owner_user_id"], owner_id);
    assert_eq!(team["is_open"], true);
    assert_eq!(team["member_count"], 1);
}

#[serial]
#[tokio::test]
async fn admin_create_team_unknown_owner_404() {
    let app = TestApp::new().await;
    let body = serde_json::json!({
        "name": "Orphan",
        "owner_user_id": "00000000-0000-4000-8000-000000000000",
    });
    let req = Request::builder()
        .method("POST")
        .uri("/admin/teams")
        .header("Content-Type", "application/json")
        .header("Authorization", format!("Bearer {}", app.admin_token))
        .body(Body::from(serde_json::to_vec(&body).unwrap()))
        .unwrap();
    let resp = app.request(req).await;
    resp.assert_status(StatusCode::NOT_FOUND);
    let json: serde_json::Value = resp.json();
    assert_eq!(json["error"], "user_not_found");
}

#[serial]
#[tokio::test]
async fn admin_create_team_validates_name() {
    let app = TestApp::new().await;
    let (_token, owner_id) = setup_user(&app, "valname@test.com").await;
    let body = serde_json::json!({"name": "   ", "owner_user_id": owner_id});
    let req = Request::builder()
        .method("POST")
        .uri("/admin/teams")
        .header("Content-Type", "application/json")
        .header("Authorization", format!("Bearer {}", app.admin_token))
        .body(Body::from(serde_json::to_vec(&body).unwrap()))
        .unwrap();
    let resp = app.request(req).await;
    resp.assert_status(StatusCode::BAD_REQUEST);
}

#[serial]
#[tokio::test]
async fn admin_add_team_member() {
    let app = TestApp::new().await;
    let (_, owner_id) = setup_user(&app, "addowner@test.com").await;
    let (_, target_id) = setup_user(&app, "addtarget@test.com").await;

    let team = admin_create_team(&app, "Add Test", &owner_id).await;
    let team_id = team["id"].as_str().unwrap().to_string();

    let body = serde_json::json!({"user_id": target_id});
    let req = Request::builder()
        .method("POST")
        .uri(format!("/admin/teams/{team_id}/members"))
        .header("Content-Type", "application/json")
        .header("Authorization", format!("Bearer {}", app.admin_token))
        .body(Body::from(serde_json::to_vec(&body).unwrap()))
        .unwrap();
    let resp = app.request(req).await;
    resp.assert_status(StatusCode::OK);
    let json: serde_json::Value = resp.json();
    assert_eq!(json["team_id"], team_id);
    assert_eq!(json["user_id"], target_id);
    assert_eq!(json["role"], "member");
}

#[serial]
#[tokio::test]
async fn admin_add_team_member_idempotent() {
    let app = TestApp::new().await;
    let (_, owner_id) = setup_user(&app, "idemowner@test.com").await;
    let (_, target_id) = setup_user(&app, "idemtarget@test.com").await;

    let team = admin_create_team(&app, "Idem Test", &owner_id).await;
    let team_id = team["id"].as_str().unwrap().to_string();

    let body = serde_json::json!({"user_id": target_id});
    let make_req = || {
        Request::builder()
            .method("POST")
            .uri(format!("/admin/teams/{team_id}/members"))
            .header("Content-Type", "application/json")
            .header("Authorization", format!("Bearer {}", app.admin_token))
            .body(Body::from(serde_json::to_vec(&body).unwrap()))
            .unwrap()
    };
    app.request(make_req()).await.assert_status(StatusCode::OK);
    // Second add — should still be 200 (return existing membership)
    let resp = app.request(make_req()).await;
    resp.assert_status(StatusCode::OK);
    let json: serde_json::Value = resp.json();
    assert_eq!(json["role"], "member");
}

#[serial]
#[tokio::test]
async fn admin_add_team_member_unknown_user_404() {
    let app = TestApp::new().await;
    let (_, owner_id) = setup_user(&app, "u404owner@test.com").await;
    let team = admin_create_team(&app, "U404", &owner_id).await;
    let team_id = team["id"].as_str().unwrap().to_string();

    let body = serde_json::json!({"user_id": "00000000-0000-4000-8000-000000000000"});
    let req = Request::builder()
        .method("POST")
        .uri(format!("/admin/teams/{team_id}/members"))
        .header("Content-Type", "application/json")
        .header("Authorization", format!("Bearer {}", app.admin_token))
        .body(Body::from(serde_json::to_vec(&body).unwrap()))
        .unwrap();
    let resp = app.request(req).await;
    resp.assert_status(StatusCode::NOT_FOUND);
    let json: serde_json::Value = resp.json();
    assert_eq!(json["error"], "user_not_found");
}

#[serial]
#[tokio::test]
async fn admin_remove_team_member() {
    let app = TestApp::new().await;
    let (_, owner_id) = setup_user(&app, "rmowner@test.com").await;
    let (_, target_id) = setup_user(&app, "rmtarget@test.com").await;

    let team = admin_create_team(&app, "Remove Test", &owner_id).await;
    let team_id = team["id"].as_str().unwrap().to_string();

    // Add target as member.
    let add_body = serde_json::json!({"user_id": target_id});
    let req = Request::builder()
        .method("POST")
        .uri(format!("/admin/teams/{team_id}/members"))
        .header("Content-Type", "application/json")
        .header("Authorization", format!("Bearer {}", app.admin_token))
        .body(Body::from(serde_json::to_vec(&add_body).unwrap()))
        .unwrap();
    app.request(req).await.assert_status(StatusCode::OK);

    // Now remove.
    let req = Request::builder()
        .method("DELETE")
        .uri(format!("/admin/teams/{team_id}/members/{target_id}"))
        .header("Authorization", format!("Bearer {}", app.admin_token))
        .body(Body::empty())
        .unwrap();
    let resp = app.request(req).await;
    resp.assert_status(StatusCode::OK);
    let json: serde_json::Value = resp.json();
    assert_eq!(json["status"], "removed");
}

#[serial]
#[tokio::test]
async fn admin_remove_team_owner_blocked() {
    let app = TestApp::new().await;
    let (_, owner_id) = setup_user(&app, "ownerrm@test.com").await;
    let team = admin_create_team(&app, "Owner Remove", &owner_id).await;
    let team_id = team["id"].as_str().unwrap().to_string();

    let req = Request::builder()
        .method("DELETE")
        .uri(format!("/admin/teams/{team_id}/members/{owner_id}"))
        .header("Authorization", format!("Bearer {}", app.admin_token))
        .body(Body::empty())
        .unwrap();
    let resp = app.request(req).await;
    resp.assert_status(StatusCode::BAD_REQUEST);
}

#[serial]
#[tokio::test]
async fn admin_remove_non_member_400() {
    let app = TestApp::new().await;
    let (_, owner_id) = setup_user(&app, "nm-owner@test.com").await;
    let (_, ghost_id) = setup_user(&app, "ghost@test.com").await;

    let team = admin_create_team(&app, "Ghost Test", &owner_id).await;
    let team_id = team["id"].as_str().unwrap().to_string();

    // ghost was never added to the team
    let req = Request::builder()
        .method("DELETE")
        .uri(format!("/admin/teams/{team_id}/members/{ghost_id}"))
        .header("Authorization", format!("Bearer {}", app.admin_token))
        .body(Body::empty())
        .unwrap();
    let resp = app.request(req).await;
    resp.assert_status(StatusCode::BAD_REQUEST);
}

#[serial]
#[tokio::test]
async fn admin_team_endpoints_require_admin() {
    let app = TestApp::new().await;
    let (regular_token, owner_id) = setup_user(&app, "nonadmin@test.com").await;

    // Create with non-admin token → 403
    let body = serde_json::json!({"name": "Sneaky", "owner_user_id": owner_id});
    let req = Request::builder()
        .method("POST")
        .uri("/admin/teams")
        .header("Content-Type", "application/json")
        .header("Authorization", format!("Bearer {regular_token}"))
        .body(Body::from(serde_json::to_vec(&body).unwrap()))
        .unwrap();
    let resp = app.request(req).await;
    resp.assert_status(StatusCode::FORBIDDEN);
}
