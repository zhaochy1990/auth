use axum::{extract::Path, extract::State, Json};
use serde::{Deserialize, Serialize};
use uuid::Uuid;

use crate::auth::middleware::AuthenticatedUser;
use crate::db::models::{Team, TeamMembership};
use crate::error::AppError;
use crate::AppState;

// --- Request / Response types ---

#[derive(Debug, Deserialize)]
pub struct CreateTeamRequest {
    pub name: String,
    pub description: Option<String>,
}

#[derive(Debug, Serialize)]
pub struct TeamResponse {
    pub id: String,
    pub name: String,
    pub description: Option<String>,
    pub owner_user_id: String,
    pub is_open: bool,
    pub member_count: u64,
    pub created_at: String,
    pub updated_at: String,
}

#[derive(Debug, Serialize)]
pub struct TeamListResponse {
    pub teams: Vec<TeamResponse>,
}

#[derive(Debug, Serialize)]
pub struct TeamMembershipResponse {
    pub team_id: String,
    pub user_id: String,
    pub role: String,
    pub joined_at: String,
}

#[derive(Debug, Serialize)]
pub struct TeamMemberInfo {
    pub user_id: String,
    pub name: Option<String>,
    pub email: Option<String>,
    pub role: String,
    pub joined_at: String,
}

#[derive(Debug, Serialize)]
pub struct TeamMembersResponse {
    pub members: Vec<TeamMemberInfo>,
}

#[derive(Debug, Serialize)]
pub struct MyTeamEntry {
    pub id: String,
    pub name: String,
    pub role: String,
    pub joined_at: String,
}

#[derive(Debug, Serialize)]
pub struct MyTeamsResponse {
    pub teams: Vec<MyTeamEntry>,
}

// --- Handlers ---

pub async fn create_team(
    user: AuthenticatedUser,
    State(state): State<AppState>,
    Json(req): Json<CreateTeamRequest>,
) -> Result<Json<TeamResponse>, AppError> {
    let name = req.name.trim().to_string();
    if name.is_empty() || name.chars().count() > 100 {
        return Err(AppError::BadRequest(
            "Team name must be 1-100 characters".to_string(),
        ));
    }

    let now = chrono::Utc::now().naive_utc();
    let team = Team {
        id: Uuid::new_v4().to_string(),
        name,
        description: req.description,
        owner_user_id: user.user_id.clone(),
        is_open: true,
        created_at: now,
        updated_at: now,
    };

    state.repo.teams().insert(&team).await?;

    let owner_membership = TeamMembership {
        team_id: team.id.clone(),
        user_id: user.user_id.clone(),
        role: "owner".to_string(),
        joined_at: now,
    };
    state
        .repo
        .team_memberships()
        .insert(&owner_membership)
        .await?;

    Ok(Json(TeamResponse {
        id: team.id,
        name: team.name,
        description: team.description,
        owner_user_id: team.owner_user_id,
        is_open: team.is_open,
        member_count: 1,
        created_at: team.created_at.to_string(),
        updated_at: team.updated_at.to_string(),
    }))
}

pub async fn list_teams(
    _user: AuthenticatedUser,
    State(state): State<AppState>,
) -> Result<Json<TeamListResponse>, AppError> {
    let teams = state.repo.teams().find_all_open().await?;

    let mut responses = Vec::with_capacity(teams.len());
    for team in teams {
        let count = state
            .repo
            .team_memberships()
            .count_by_team(&team.id)
            .await?;
        responses.push(TeamResponse {
            id: team.id,
            name: team.name,
            description: team.description,
            owner_user_id: team.owner_user_id,
            is_open: team.is_open,
            member_count: count,
            created_at: team.created_at.to_string(),
            updated_at: team.updated_at.to_string(),
        });
    }

    Ok(Json(TeamListResponse { teams: responses }))
}

pub async fn get_team(
    _user: AuthenticatedUser,
    State(state): State<AppState>,
    Path(team_id): Path<String>,
) -> Result<Json<TeamResponse>, AppError> {
    let team = state
        .repo
        .teams()
        .find_by_id(&team_id)
        .await?
        .ok_or(AppError::TeamNotFound)?;

    let count = state
        .repo
        .team_memberships()
        .count_by_team(&team.id)
        .await?;

    Ok(Json(TeamResponse {
        id: team.id,
        name: team.name,
        description: team.description,
        owner_user_id: team.owner_user_id,
        is_open: team.is_open,
        member_count: count,
        created_at: team.created_at.to_string(),
        updated_at: team.updated_at.to_string(),
    }))
}

pub async fn join_team(
    user: AuthenticatedUser,
    State(state): State<AppState>,
    Path(team_id): Path<String>,
) -> Result<Json<TeamMembershipResponse>, AppError> {
    let team = state
        .repo
        .teams()
        .find_by_id(&team_id)
        .await?
        .ok_or(AppError::TeamNotFound)?;

    if !team.is_open {
        return Err(AppError::TeamNotOpen);
    }

    // Idempotent: if already a member, return existing.
    if let Some(existing) = state
        .repo
        .team_memberships()
        .find(&team_id, &user.user_id)
        .await?
    {
        return Ok(Json(TeamMembershipResponse {
            team_id: existing.team_id,
            user_id: existing.user_id,
            role: existing.role,
            joined_at: existing.joined_at.to_string(),
        }));
    }

    let now = chrono::Utc::now().naive_utc();
    let membership = TeamMembership {
        team_id: team_id.clone(),
        user_id: user.user_id.clone(),
        role: "member".to_string(),
        joined_at: now,
    };
    state.repo.team_memberships().insert(&membership).await?;

    Ok(Json(TeamMembershipResponse {
        team_id: membership.team_id,
        user_id: membership.user_id,
        role: membership.role,
        joined_at: membership.joined_at.to_string(),
    }))
}

pub async fn leave_team(
    user: AuthenticatedUser,
    State(state): State<AppState>,
    Path(team_id): Path<String>,
) -> Result<Json<serde_json::Value>, AppError> {
    let team = state
        .repo
        .teams()
        .find_by_id(&team_id)
        .await?
        .ok_or(AppError::TeamNotFound)?;

    let existing = state
        .repo
        .team_memberships()
        .find(&team_id, &user.user_id)
        .await?
        .ok_or(AppError::BadRequest(
            "Not a member of this team".to_string(),
        ))?;

    if team.owner_user_id == user.user_id {
        let count = state
            .repo
            .team_memberships()
            .count_by_team(&team_id)
            .await?;
        if count <= 1 {
            return Err(AppError::OwnerCannotLeaveAsLastMember);
        }
    }

    state
        .repo
        .team_memberships()
        .delete(&team_id, &user.user_id)
        .await?;

    let _ = existing; // silence unused
    Ok(Json(serde_json::json!({"status": "left"})))
}

pub async fn list_members(
    _user: AuthenticatedUser,
    State(state): State<AppState>,
    Path(team_id): Path<String>,
) -> Result<Json<TeamMembersResponse>, AppError> {
    // Verify team exists
    let _team = state
        .repo
        .teams()
        .find_by_id(&team_id)
        .await?
        .ok_or(AppError::TeamNotFound)?;

    let memberships = state
        .repo
        .team_memberships()
        .find_all_by_team(&team_id)
        .await?;

    let mut members = Vec::with_capacity(memberships.len());
    for m in memberships {
        let user = state.repo.users().find_by_id(&m.user_id).await?;
        let (name, email) = match user {
            Some(u) => (u.name, u.email),
            None => (None, None),
        };
        members.push(TeamMemberInfo {
            user_id: m.user_id,
            name,
            email,
            role: m.role,
            joined_at: m.joined_at.to_string(),
        });
    }

    Ok(Json(TeamMembersResponse { members }))
}

pub async fn list_my_teams(
    user: AuthenticatedUser,
    State(state): State<AppState>,
) -> Result<Json<MyTeamsResponse>, AppError> {
    let memberships = state
        .repo
        .team_memberships()
        .find_all_by_user(&user.user_id)
        .await?;

    let mut teams = Vec::with_capacity(memberships.len());
    for m in memberships {
        if let Some(team) = state.repo.teams().find_by_id(&m.team_id).await? {
            teams.push(MyTeamEntry {
                id: team.id,
                name: team.name,
                role: m.role,
                joined_at: m.joined_at.to_string(),
            });
        }
    }

    Ok(Json(MyTeamsResponse { teams }))
}
