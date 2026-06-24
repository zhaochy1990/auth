// Auth
export interface LoginRequest {
  email: string;
  password: string;
}

export interface TokenResponse {
  access_token: string;
  refresh_token: string;
  token_type: string;
  expires_in: number;
}

// Applications
export interface Application {
  id: string;
  name: string;
  client_id: string;
  redirect_uris: string[];
  allowed_scopes: string[];
  is_active: boolean;
  created_at: string;
}

export interface CreateApplicationRequest {
  name: string;
  redirect_uris: string[];
  allowed_scopes: string[];
}

export interface CreateApplicationResponse {
  id: string;
  name: string;
  client_id: string;
  client_secret: string;
  redirect_uris: string[];
  allowed_scopes: string[];
}

export interface UpdateApplicationRequest {
  name?: string;
  redirect_uris?: string[];
  allowed_scopes?: string[];
  is_active?: boolean;
}

export interface RotateSecretResponse {
  client_id: string;
  client_secret: string;
}

// Providers
export interface Provider {
  id: string;
  provider_id: string;
  config: Record<string, unknown>;
  is_active: boolean;
  created_at: string;
}

export interface AddProviderRequest {
  provider_id: string;
  config: Record<string, unknown>;
}

// Users

// Membership tier (entitlement level), independent of `role`. Extend with
// 'vip2' | 'vip3' as higher tiers are introduced.
export type MembershipTier = 'regular' | 'vip1';

export interface LoginRecord {
  at: string;
  ip: string;
}

export interface User {
  id: string;
  email: string | null;
  name: string | null;
  avatar_url: string | null;
  email_verified: boolean;
  role: string;
  membership: MembershipTier;
  membership_expires_at: string | null;
  is_active: boolean;
  note: string | null;
  created_at: string;
  updated_at: string;
  last_login_at: string | null;
  recent_logins: LoginRecord[];
}

export interface UserListResponse {
  users: User[];
  total: number;
  page: number;
  per_page: number;
}

export interface UpdateUserRequest {
  name?: string;
  role?: string;
  membership?: MembershipTier;
  // ISO date/datetime to set the paid-tier expiry; empty string clears it.
  membership_expires_at?: string;
  is_active?: boolean;
  note?: string;
}

export interface CreateUserRequest {
  email: string;
  password: string;
  name?: string;
  role?: string;
  membership?: MembershipTier;
}

export interface ResetUserPasswordRequest {
  password: string;
  revoke_sessions?: boolean;
}

export interface ResetUserPasswordResponse {
  user_id: string;
  revoked_sessions: boolean;
}

export interface UserAccount {
  id: string;
  provider_id: string;
  provider_account_id: string | null;
  created_at: string;
}

// Invite Codes
export type InviteCodeKind = 'single_use' | 'long_term';

export interface InviteCode {
  id: string;
  code: string;
  created_by: string;
  created_at: string;
  used_at: string | null;
  used_by: string | null;
  is_revoked: boolean;
  kind: InviteCodeKind;
  // Membership tier granted on registration, if any.
  grants_membership: MembershipTier | null;
  // Validity in days of the granted membership; null means permanent.
  grants_membership_days: number | null;
}

// Teams
export interface Team {
  id: string;
  name: string;
  description: string | null;
  owner_user_id: string;
  is_open: boolean;
  member_count: number;
  created_at: string;
  updated_at: string;
}

export interface TeamMember {
  user_id: string;
  name: string | null;
  email: string | null;
  role: string;
  joined_at: string;
}

export interface AdminCreateTeamRequest {
  name: string;
  description?: string;
  owner_user_id: string;
  is_open?: boolean;
}

export interface AdminAddMemberRequest {
  user_id: string;
  role?: 'member' | 'owner';
}

export interface TeamMembership {
  team_id: string;
  user_id: string;
  role: string;
  joined_at: string;
}

// Stats
export interface Stats {
  applications: {
    total: number;
    active: number;
    inactive: number;
  };
  users: {
    total: number;
    recent: number;
  };
}

// JWT Claims (decoded client-side)
export interface JwtPayload {
  sub: string;
  aud: string;
  iss: string;
  exp: number;
  iat: number;
  scopes: string[];
  role: string;
  /// Membership tier embedded in the access token.
  membership?: MembershipTier;
  /// Display name of the user, when set on their profile.
  name?: string;
}
