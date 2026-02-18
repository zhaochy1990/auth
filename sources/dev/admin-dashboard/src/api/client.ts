import axios from 'axios';
import type { JwtPayload } from './types';

const CLIENT_ID = import.meta.env.VITE_API_CLIENT_ID || '';
const BASE_URL = import.meta.env.VITE_API_BASE_URL || '';

const client = axios.create({
  baseURL: BASE_URL,
  headers: { 'Content-Type': 'application/json' },
});

// Request interceptor: attach Bearer token for admin routes
client.interceptors.request.use((config) => {
  const token = sessionStorage.getItem('access_token');
  if (token && config.url?.startsWith('/admin')) {
    config.headers.Authorization = `Bearer ${token}`;
  }
  return config;
});

// Response interceptor: auto-refresh on 401
let refreshPromise: Promise<string> | null = null;

client.interceptors.response.use(
  (res) => res,
  async (error) => {
    const original = error.config;
    if (error.response?.status === 401 && !original._retry && original.url?.startsWith('/admin')) {
      original._retry = true;
      try {
        const newToken = await refreshAccessToken();
        original.headers.Authorization = `Bearer ${newToken}`;
        return client(original);
      } catch {
        sessionStorage.clear();
        window.location.href = '/login';
        return Promise.reject(error);
      }
    }
    return Promise.reject(error);
  },
);

async function refreshAccessToken(): Promise<string> {
  if (refreshPromise) return refreshPromise;

  refreshPromise = (async () => {
    try {
      const refreshToken = sessionStorage.getItem('refresh_token');
      if (!refreshToken) throw new Error('No refresh token');

      const res = await axios.post(
        `${BASE_URL}/api/auth/refresh`,
        { refresh_token: refreshToken },
        { headers: { 'X-Client-Id': CLIENT_ID } },
      );

      const { access_token, refresh_token } = res.data;
      sessionStorage.setItem('access_token', access_token);
      sessionStorage.setItem('refresh_token', refresh_token);
      return access_token as string;
    } finally {
      refreshPromise = null;
    }
  })();

  return refreshPromise;
}

// Proactive refresh: schedule before expiry
export function scheduleTokenRefresh() {
  const token = sessionStorage.getItem('access_token');
  if (!token) return;

  try {
    const payload = decodeJwt(token);
    const msUntilExpiry = payload.exp * 1000 - Date.now();
    // Refresh 60s before expiry
    const delay = Math.max(msUntilExpiry - 60_000, 1_000);
    setTimeout(async () => {
      try {
        await refreshAccessToken();
        scheduleTokenRefresh();
      } catch { /* will redirect on next 401 */ }
    }, delay);
  } catch { /* invalid token */ }
}

export function decodeJwt(token: string): JwtPayload {
  const base64Url = token.split('.')[1];
  const base64 = base64Url.replace(/-/g, '+').replace(/_/g, '/');
  const json = decodeURIComponent(
    atob(base64)
      .split('')
      .map((c) => '%' + ('00' + c.charCodeAt(0).toString(16)).slice(-2))
      .join(''),
  );
  return JSON.parse(json);
}

export function getClientId(): string {
  return CLIENT_ID;
}

export default client;
