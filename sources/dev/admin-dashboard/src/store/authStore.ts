import { create } from 'zustand';
import client, { decodeJwt, getClientId, scheduleTokenRefresh } from '../api/client';

interface AuthState {
  accessToken: string | null;
  refreshToken: string | null;
  userId: string | null;
  role: string | null;
  isAuthenticated: boolean;

  login: (email: string, password: string) => Promise<void>;
  logout: () => void;
  hydrate: () => void;
}

export const useAuthStore = create<AuthState>((set) => ({
  accessToken: null,
  refreshToken: null,
  userId: null,
  role: null,
  isAuthenticated: false,

  login: async (email: string, password: string) => {
    const res = await client.post(
      '/api/auth/login',
      { email, password },
      { headers: { 'X-Client-Id': getClientId() } },
    );

    const { access_token, refresh_token } = res.data;
    const payload = decodeJwt(access_token);

    if (payload.role !== 'admin') {
      throw new Error('INSUFFICIENT_PERMISSIONS');
    }

    sessionStorage.setItem('access_token', access_token);
    sessionStorage.setItem('refresh_token', refresh_token);

    set({
      accessToken: access_token,
      refreshToken: refresh_token,
      userId: payload.sub,
      role: payload.role,
      isAuthenticated: true,
    });

    scheduleTokenRefresh();
  },

  logout: () => {
    sessionStorage.clear();
    set({
      accessToken: null,
      refreshToken: null,
      userId: null,
      role: null,
      isAuthenticated: false,
    });
  },

  hydrate: () => {
    const accessToken = sessionStorage.getItem('access_token');
    const refreshToken = sessionStorage.getItem('refresh_token');

    if (accessToken && refreshToken) {
      try {
        const payload = decodeJwt(accessToken);
        if (payload.role === 'admin' && payload.exp * 1000 > Date.now()) {
          set({
            accessToken,
            refreshToken,
            userId: payload.sub,
            role: payload.role,
            isAuthenticated: true,
          });
          scheduleTokenRefresh();
          return;
        }
      } catch { /* invalid token, fall through */ }
    }

    sessionStorage.clear();
    set({
      accessToken: null,
      refreshToken: null,
      userId: null,
      role: null,
      isAuthenticated: false,
    });
  },
}));
