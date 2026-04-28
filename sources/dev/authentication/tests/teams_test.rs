// Requires Azurite running on port 10002 (cargo test will fail otherwise).

mod common;

use axum::body::Body;
use axum::http::{Request, StatusCode};
use common::TestApp;
use serial_test::serial;

/// Helper: create app, register user, return (CreatedApp, access_token, user_id).
async fn setup_user(app: &TestApp, email: &str) -> (common::CreatedApp, String, String) {
    let created = app
        .admin_create_app(
            &format!("Teams App {email}"),
            &["https://example.com/cb"],
            &["openid", "profile"],
        )
        .await;

    let resp = app
        .register_user(&created.client_id, email, "Password1!")
        .await;
    resp.assert_status(StatusCode::CREATED);
    let json: serde_json::Value = resp.json();
    let access_token = json["access_token"].as_str().unwrap().to_string();
    let user_id = json["user_id"].as_str().unwrap().to_string();

    (created, access_token, user_id)
}

async fn create_team(app: &TestApp, token: &str, name: &str) -> serde_json::Value {
    let body = serde_json::json!({"name": name, "description": "desc"});
    let req = Request::builder()
        .method("POST")
        .uri("/api/teams")
        .header("Content-Type", "application/json")
        .header("Authorization", format!("Bearer {token}"))
        .body(Body::from(serde_json::to_vec(&body).unwrap()))
        .unwrap();
    let resp = app.request(req).await;
    resp.assert_status(StatusCode::OK);
    resp.json()
}

#[serial]
#[tokio::test]
async fn create_team_success() {
    let app = TestApp::new().await;
    let (_, token, user_id) = setup_user(&app, "owner@test.com").await;

    let json = create_team(&app, &token, "Alpha Team").await;
    assert_eq!(json["name"], "Alpha Team");
    assert_eq!(json["owner_user_id"], user_id);
    assert_eq!(json["is_open"], true);
    assert_eq!(json["member_count"], 1);
    assert!(json["id"].as_str().unwrap().len() > 10);
}

#[serial]
#[tokio::test]
async fn create_team_name_validation() {
    let app = TestApp::new().await;
    let (_, token, _) = setup_user(&app, "v@test.com").await;

    // empty name
    let body = serde_json::json!({"name": "   "});
    let req = Request::builder()
        .method("POST")
        .uri("/api/teams")
        .header("Content-Type", "application/json")
        .header("Authorization", format!("Bearer {token}"))
        .body(Body::from(serde_json::to_vec(&body).unwrap()))
        .unwrap();
    let resp = app.request(req).await;
    resp.assert_status(StatusCode::BAD_REQUEST);
}

#[serial]
#[tokio::test]
async fn list_teams() {
    let app = TestApp::new().await;
    let (_, token, _) = setup_user(&app, "lister@test.com").await;

    create_team(&app, &token, "T1").await;
    create_team(&app, &token, "T2").await;

    let req = Request::builder()
        .method("GET")
        .uri("/api/teams")
        .header("Authorization", format!("Bearer {token}"))
        .body(Body::empty())
        .unwrap();
    let resp = app.request(req).await;
    resp.assert_status(StatusCode::OK);
    let json: serde_json::Value = resp.json();
    let teams = json["teams"].as_array().unwrap();
    assert!(teams.len() >= 2);
}

#[serial]
#[tokio::test]
async fn get_team_404() {
    let app = TestApp::new().await;
    let (_, token, _) = setup_user(&app, "x@test.com").await;

    let req = Request::builder()
        .method("GET")
        .uri("/api/teams/nonexistent-id")
        .header("Authorization", format!("Bearer {token}"))
        .body(Body::empty())
        .unwrap();
    let resp = app.request(req).await;
    resp.assert_status(StatusCode::NOT_FOUND);
    let json: serde_json::Value = resp.json();
    assert_eq!(json["error"], "team_not_found");
}

#[serial]
#[tokio::test]
async fn join_idempotent() {
    let app = TestApp::new().await;
    let (_, owner_token, _) = setup_user(&app, "owner@test.com").await;
    let team = create_team(&app, &owner_token, "Joinable").await;
    let team_id = team["id"].as_str().unwrap().to_string();

    let (_, joiner_token, _) = setup_user(&app, "joiner@test.com").await;

    // First join
    let req = Request::builder()
        .method("POST")
        .uri(format!("/api/teams/{team_id}/join"))
        .header("Authorization", format!("Bearer {joiner_token}"))
        .body(Body::empty())
        .unwrap();
    let resp = app.request(req).await;
    resp.assert_status(StatusCode::OK);
    let json: serde_json::Value = resp.json();
    assert_eq!(json["role"], "member");

    // Second join (idempotent)
    let req = Request::builder()
        .method("POST")
        .uri(format!("/api/teams/{team_id}/join"))
        .header("Authorization", format!("Bearer {joiner_token}"))
        .body(Body::empty())
        .unwrap();
    let resp = app.request(req).await;
    resp.assert_status(StatusCode::OK);
}

#[serial]
#[tokio::test]
async fn leave_owner_blocked() {
    let app = TestApp::new().await;
    let (_, owner_token, _) = setup_user(&app, "owner@test.com").await;
    let team = create_team(&app, &owner_token, "Solo").await;
    let team_id = team["id"].as_str().unwrap().to_string();

    let req = Request::builder()
        .method("POST")
        .uri(format!("/api/teams/{team_id}/leave"))
        .header("Authorization", format!("Bearer {owner_token}"))
        .body(Body::empty())
        .unwrap();
    let resp = app.request(req).await;
    resp.assert_status(StatusCode::BAD_REQUEST);
    let json: serde_json::Value = resp.json();
    assert_eq!(json["error"], "owner_cannot_leave_as_last_member");
}

#[serial]
#[tokio::test]
async fn members_list() {
    let app = TestApp::new().await;
    let (_, owner_token, owner_id) = setup_user(&app, "owner@test.com").await;
    let team = create_team(&app, &owner_token, "M1").await;
    let team_id = team["id"].as_str().unwrap().to_string();

    let req = Request::builder()
        .method("GET")
        .uri(format!("/api/teams/{team_id}/members"))
        .header("Authorization", format!("Bearer {owner_token}"))
        .body(Body::empty())
        .unwrap();
    let resp = app.request(req).await;
    resp.assert_status(StatusCode::OK);
    let json: serde_json::Value = resp.json();
    let members = json["members"].as_array().unwrap();
    assert_eq!(members.len(), 1);
    assert_eq!(members[0]["user_id"], owner_id);
    assert_eq!(members[0]["role"], "owner");
}

#[serial]
#[tokio::test]
async fn my_teams() {
    let app = TestApp::new().await;
    let (_, owner_token, _) = setup_user(&app, "me@test.com").await;
    create_team(&app, &owner_token, "MyTeam1").await;
    create_team(&app, &owner_token, "MyTeam2").await;

    let req = Request::builder()
        .method("GET")
        .uri("/api/users/me/teams")
        .header("Authorization", format!("Bearer {owner_token}"))
        .body(Body::empty())
        .unwrap();
    let resp = app.request(req).await;
    resp.assert_status(StatusCode::OK);
    let json: serde_json::Value = resp.json();
    let teams = json["teams"].as_array().unwrap();
    assert_eq!(teams.len(), 2);
    for t in teams {
        assert_eq!(t["role"], "owner");
    }
}
