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
  UpdateUserRequest,
  UserAccount,
  Stats,
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

export const getUser = (id: string) =>
  client.get<User>(`/admin/users/${id}`).then((r) => r.data);

export const updateUser = (id: string, data: UpdateUserRequest) =>
  client.patch<User>(`/admin/users/${id}`, data).then((r) => r.data);

export const getUserAccounts = (id: string) =>
  client.get<UserAccount[]>(`/admin/users/${id}/accounts`).then((r) => r.data);

export const adminUnlinkAccount = (userId: string, providerId: string) =>
  client.delete(`/admin/users/${userId}/accounts/${providerId}`).then((r) => r.data);

// Stats
export const getStats = () =>
  client.get<Stats>('/admin/stats').then((r) => r.data);
