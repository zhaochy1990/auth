pub mod admin;
pub mod auth;
pub mod oauth2;
pub mod teams;
pub mod user;

use crate::db::models::{MembershipTier, User};
use crate::db::repository::Repository;

/// Returns the user's effective membership tier, lazily downgrading an expired
/// paid tier to `Regular` and persisting that change. Persisting is best-effort:
/// a failure is logged but does not block token issuance. Call before embedding
/// the tier in a freshly issued access token.
pub async fn resolve_membership(repo: &dyn Repository, user: &mut User) -> MembershipTier {
    let now = chrono::Utc::now().naive_utc();
    if user.is_membership_expired(now) {
        user.membership = MembershipTier::Regular;
        user.membership_expires_at = None;
        user.updated_at = now;
        if let Err(e) = repo.users().update(user).await {
            tracing::warn!(error = %e, user_id = %user.id, "failed to persist membership downgrade");
        }
    }
    user.membership
}
