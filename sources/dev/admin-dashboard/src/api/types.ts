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
  is_active: boolean;
  created_at: string;
}

export interface AddProviderRequest {
  provider_id: string;
  config: Record<string, unknown>;
}

// Users
export interface User {
  id: string;
  email: string | null;
  name: string | null;
  avatar_url: string | null;
  email_verified: boolean;
  role: string;
  is_active: boolean;
  created_at: string;
  updated_at: string;
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
  is_active?: boolean;
}

export interface CreateUserRequest {
  email: string;
  password: string;
  name?: string;
  role?: string;
}

export interface UserAccount {
  id: string;
  provider_id: string;
  provider_account_id: string | null;
  created_at: string;
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
}
