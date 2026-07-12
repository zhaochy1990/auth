import client from './client';
import type {
  Application,
  CreateApplicationRequest,
  CreateApplicationResponse,
  UpdateApplicationRequest,
  RotateSecretResponse,
  Provider,
  AddProviderRequest,
  UserListResponse,
  User,
  CreateUserRequest,
  UpdateUserRequest,
  ResetUserPasswordRequest,
  ResetUserPasswordResponse,
  UserAccount,
  Stats,
  InviteCode,
  InviteCodeKind,
  MembershipTier,
  Team,
  TeamMember,
  TeamMembership,
  AdminCreateTeamRequest,
  AdminAddMemberRequest,
} from './types';

// Applications
export const listApplications = () =>
  client.get<Application[]>('/admin/applications').then((r) => r.data);

export const createApplication = (data: CreateApplicationRequest) =>
  client.post<CreateApplicationResponse>('/admin/applications', data).then((r) => r.data);

export const updateApplication = (id: string, data: UpdateApplicationRequest) =>
  client.patch<Application>(`/admin/applications/${id}`, data).then((r) => r.data);

export const rotateSecret = (id: string) =>
  client.post<RotateSecretResponse>(`/admin/applications/${id}/rotate-secret`).then((r) => r.data);

// Providers
export const listProviders = (appId: string) =>
  client.get<Provider[]>(`/admin/applications/${appId}/providers`).then((r) => r.data);

export const addProvider = (appId: string, data: AddProviderRequest) =>
  client.post<Provider>(`/admin/applications/${appId}/providers`, data).then((r) => r.data);

export const removeProvider = (appId: string, providerId: string) =>
  client.delete(`/admin/applications/${appId}/providers/${providerId}`).then((r) => r.data);

// Users
export const listUsers = (params: { page?: number; per_page?: number; search?: string }) =>
  client.get<UserListResponse>('/admin/users', { params }).then((r) => r.data);

export const createUser = (data: CreateUserRequest) =>
  client.post<User>('/admin/users', data).then((r) => r.data);

export const getUser = (id: string) =>
  client.get<User>(`/admin/users/${id}`).then((r) => r.data);

export const updateUser = (id: string, data: UpdateUserRequest) =>
  client.patch<User>(`/admin/users/${id}`, data).then((r) => r.data);

export const deleteUser = (id: string) =>
  client.delete(`/admin/users/${id}`).then((r) => r.data);

export const getUserAccounts = (id: string) =>
  client.get<UserAccount[]>(`/admin/users/${id}/accounts`).then((r) => r.data);

export const adminUnlinkAccount = (userId: string, providerId: string) =>
  client.delete(`/admin/users/${userId}/accounts/${providerId}`).then((r) => r.data);

export const resetUserPassword = (id: string, data: ResetUserPasswordRequest) =>
  client
    .post<ResetUserPasswordResponse>(`/admin/users/${id}/reset-password`, data)
    .then((r) => r.data);

// Stats
export const getStats = () =>
  client.get<Stats>('/admin/stats').then((r) => r.data);

// Invite Codes
export const listInviteCodes = () =>
  client.get<InviteCode[]>('/admin/invite-codes').then((r) => r.data);

export const createInviteCode = (
  params: {
    kind?: InviteCodeKind;
    grants_membership?: MembershipTier;
    grants_membership_days?: number;
    marks_test_user?: boolean;
  } = {}
) =>
  client
    .post<InviteCode>('/admin/invite-codes', null, {
      params: {
        kind: params.kind ?? 'single_use',
        grants_membership: params.grants_membership,
        grants_membership_days: params.grants_membership_days,
        marks_test_user: params.marks_test_user,
      },
    })
    .then((r) => r.data);

export const revokeInviteCode = (code: string) =>
  client.delete(`/admin/invite-codes/${code}`).then((r) => r.data);

// Teams (mix of user-facing reads + admin-only mutations)
export const listTeams = () =>
  client.get<{ teams: Team[] }>('/api/teams').then((r) => r.data.teams);

export const getTeam = (id: string) =>
  client.get<Team>(`/api/teams/${id}`).then((r) => r.data);

export const getTeamMembers = (id: string) =>
  client.get<{ members: TeamMember[] }>(`/api/teams/${id}/members`).then((r) => r.data.members);

export const adminCreateTeam = (data: AdminCreateTeamRequest) =>
  client.post<Team>('/admin/teams', data).then((r) => r.data);

export const adminAddTeamMember = (teamId: string, data: AdminAddMemberRequest) =>
  client.post<TeamMembership>(`/admin/teams/${teamId}/members`, data).then((r) => r.data);

export const adminRemoveTeamMember = (teamId: string, userId: string) =>
  client.delete(`/admin/teams/${teamId}/members/${userId}`).then((r) => r.data);
