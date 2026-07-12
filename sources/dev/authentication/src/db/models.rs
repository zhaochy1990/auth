use chrono::NaiveDateTime;
use serde::{Deserialize, Serialize};

/// Membership tier (entitlement level), orthogonal to `role` (which is for
/// authorization: admin vs user). `Regular` is the free default; paid tiers
/// (`Vip1`, and future `Vip2`/`Vip3`) may carry an expiry via
/// [`User::membership_expires_at`]. Stored snake_case ("regular", "vip1").
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum MembershipTier {
    #[default]
    Regular,
    Vip1,
    // Future tiers: Vip2, Vip3, ...
}

impl MembershipTier {
    /// The snake_case wire/storage representation.
    pub fn as_str(&self) -> &'static str {
        match self {
            MembershipTier::Regular => "regular",
            MembershipTier::Vip1 => "vip1",
        }
    }

    /// Parses the snake_case representation, falling back to `Regular` for
    /// unknown values so old/foreign rows never fail to deserialize.
    pub fn from_str_lenient(s: &str) -> Self {
        match s {
            "vip1" => MembershipTier::Vip1,
            _ => MembershipTier::Regular,
        }
    }

    /// Whether this is a paid tier (anything other than `Regular`).
    pub fn is_paid(&self) -> bool {
        !matches!(self, MembershipTier::Regular)
    }
}

/// Invite code reuse policy.
///
/// `SingleUse` codes are consumed by the first successful registration and then
/// rejected (the historical behavior). `LongTerm` codes can be used by any number
/// of registrations and are never marked used; they are disabled only via revoke.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum InviteCodeKind {
    #[default]
    SingleUse,
    LongTerm,
}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct InviteCode {
    pub id: String,
    pub code: String,
    pub created_by: String,
    pub created_at: NaiveDateTime,
    pub used_at: Option<NaiveDateTime>,
    pub used_by: Option<String>,
    pub is_revoked: bool,
    /// Reuse policy. Defaults to single-use for back-compat with rows that predate this field.
    #[serde(default)]
    pub kind: InviteCodeKind,
    /// If set, registering with this code grants the given membership tier.
    /// Absent (or `Regular`) means the code grants no paid membership.
    #[serde(default)]
    pub grants_membership: Option<MembershipTier>,
    /// Validity (in days) of the granted membership, counted from registration.
    /// `None` together with a set `grants_membership` means a permanent grant.
    #[serde(default)]
    pub grants_membership_days: Option<i64>,
    /// Whether users created with this invite code should be marked as test users.
    /// Defaults to false for invite codes that predate this field.
    #[serde(default)]
    pub marks_test_user: bool,
}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct Application {
    pub id: String,
    pub name: String,
    pub client_id: String,
    pub client_secret_hash: String,
    pub redirect_uris: String,
    pub allowed_scopes: String,
    pub is_active: bool,
    pub created_at: NaiveDateTime,
    pub updated_at: NaiveDateTime,
}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct AppProvider {
    pub id: String,
    pub app_id: String,
    pub provider_id: String,
    pub config: String,
    pub is_active: bool,
    pub created_at: NaiveDateTime,
}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct LoginRecord {
    pub at: NaiveDateTime,
    pub ip: String,
}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct User {
    pub id: String,
    pub email: Option<String>,
    pub name: Option<String>,
    pub avatar_url: Option<String>,
    pub email_verified: bool,
    pub role: String,
    pub is_active: bool,
    /// Admin-only free-form note about the user. Not exposed via the
    /// user-facing `/api/users/me` endpoints.
    pub note: Option<String>,
    pub created_at: NaiveDateTime,
    pub updated_at: NaiveDateTime,
    /// Most recent successful login timestamp.
    #[serde(default)]
    pub last_login_at: Option<NaiveDateTime>,
    /// The user's last 3 login records (most recent first), each with timestamp + IP.
    #[serde(default)]
    pub recent_logins: Vec<LoginRecord>,
    /// The invite code this user registered with, if registration was invite-gated.
    /// Backend-only (not surfaced via user-facing APIs). Absent on users predating this field.
    #[serde(default)]
    pub invite_code: Option<String>,
    /// Admin-visible marker for non-production/test accounts. Defaults to false
    /// for users that predate this field.
    #[serde(default)]
    pub is_test_user: bool,
    /// Membership tier (entitlement level), independent of `role`. Defaults to
    /// `Regular` for rows that predate this field.
    #[serde(default)]
    pub membership: MembershipTier,
    /// When a paid membership expires. `None` means no expiry (permanent grant
    /// or a `Regular` user). After this instant the tier is treated as `Regular`.
    #[serde(default)]
    pub membership_expires_at: Option<NaiveDateTime>,
}

impl User {
    /// Whether a paid membership has lapsed as of `now`. `Regular` users and
    /// paid memberships without an expiry are never considered expired.
    pub fn is_membership_expired(&self, now: NaiveDateTime) -> bool {
        self.membership.is_paid()
            && self
                .membership_expires_at
                .is_some_and(|expires_at| expires_at <= now)
    }

    /// The effective tier as of `now`: the stored tier, or `Regular` if the
    /// paid membership has expired.
    pub fn effective_membership(&self, now: NaiveDateTime) -> MembershipTier {
        if self.is_membership_expired(now) {
            MembershipTier::Regular
        } else {
            self.membership
        }
    }
}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct Account {
    pub id: String,
    pub user_id: String,
    pub provider_id: String,
    pub provider_account_id: Option<String>,
    pub credential: Option<String>,
    pub provider_metadata: String,
    pub created_at: NaiveDateTime,
    pub updated_at: NaiveDateTime,
}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct AuthorizationCode {
    pub code: String,
    pub app_id: String,
    pub user_id: String,
    pub redirect_uri: String,
    pub scopes: String,
    pub code_challenge: Option<String>,
    pub code_challenge_method: Option<String>,
    pub expires_at: NaiveDateTime,
    pub used: bool,
    pub created_at: NaiveDateTime,
}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct RefreshToken {
    pub id: String,
    pub user_id: String,
    pub app_id: String,
    pub token_hash: String,
    pub scopes: String,
    pub device_id: Option<String>,
    pub expires_at: NaiveDateTime,
    pub revoked: bool,
    pub created_at: NaiveDateTime,
}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct Team {
    pub id: String,
    pub name: String,
    pub description: Option<String>,
    pub owner_user_id: String,
    pub is_open: bool,
    pub created_at: NaiveDateTime,
    pub updated_at: NaiveDateTime,
}

#[derive(Clone, Debug, Serialize, Deserialize)]
pub struct TeamMembership {
    pub team_id: String,
    pub user_id: String,
    pub role: String, // "owner" | "member"
    pub joined_at: NaiveDateTime,
}
