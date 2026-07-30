// Typed REST client for the NexusVPN control plane API (api/openapi.yaml).
//
// Responsibilities:
//  - attach the JWT access token as a Bearer header
//  - on a 401 response, attempt exactly one POST /auth/refresh using the
//    stored refresh token, then retry the original request once
//  - if the refresh also fails, clear the session and notify the app
//    (lib/auth-events.ts) so the router can redirect to /login
//  - surface API errors as a typed ApiError (code + message + http status)

import {
  clearStoredTokens,
  getStoredTokens,
  setStoredTokens,
  type StoredTokens,
} from './token-storage';
import { emitSessionExpired } from './auth-events';
import type {
  ApiErrorBody,
  AuditLog,
  ConnectionLog,
  DashboardStats,
  Device,
  DeviceRegisterRequest,
  JoinNetworkRequest,
  LoginRequest,
  Member,
  MfaEnableResponse,
  Network,
  NetworkCreateRequest,
  NetworkRole,
  RegisterRequest,
  RotateInviteResponse,
  TokenPair,
  User,
} from './types';

export const API_BASE_URL: string =
  (import.meta.env.VITE_API_BASE_URL as string | undefined) ?? '/api/v1';

export class ApiError extends Error {
  code: string;
  status: number;

  constructor(code: string, message: string, status: number) {
    super(message);
    this.name = 'ApiError';
    this.code = code;
    this.status = status;
  }
}

interface RequestOptions {
  method?: 'GET' | 'POST' | 'PATCH' | 'DELETE' | 'PUT';
  body?: unknown;
  /** Skip attaching the Authorization header (login/register/refresh/etc). */
  skipAuth?: boolean;
  query?: Record<string, string | undefined>;
}

let refreshPromise: Promise<StoredTokens> | null = null;

async function performRefresh(): Promise<StoredTokens> {
  if (refreshPromise) return refreshPromise;

  refreshPromise = (async () => {
    const current = getStoredTokens();
    if (!current) {
      throw new ApiError('NO_REFRESH_TOKEN', 'No refresh token available', 401);
    }
    const res = await fetch(`${API_BASE_URL}/auth/refresh`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ refreshToken: current.refreshToken }),
    });
    if (!res.ok) {
      throw new ApiError('REFRESH_FAILED', 'Session refresh failed', res.status);
    }
    const pair = (await res.json()) as TokenPair;
    return setStoredTokens(pair);
  })();

  try {
    return await refreshPromise;
  } finally {
    refreshPromise = null;
  }
}

function buildUrl(path: string, query?: Record<string, string | undefined>): string {
  const url = new URL(`${API_BASE_URL}${path}`, window.location.origin);
  if (query) {
    for (const [key, value] of Object.entries(query)) {
      if (value !== undefined && value !== '') url.searchParams.set(key, value);
    }
  }
  return url.toString();
}

async function parseErrorBody(res: Response): Promise<{ code: string; message: string }> {
  try {
    const body = (await res.json()) as ApiErrorBody;
    if (body?.error?.message) {
      return { code: body.error.code ?? 'UNKNOWN', message: body.error.message };
    }
  } catch {
    // response had no / non-JSON body
  }
  return { code: `HTTP_${res.status}`, message: res.statusText || 'Request failed' };
}

async function request<T>(
  path: string,
  options: RequestOptions = {},
  isRetry = false,
): Promise<T> {
  const { method = 'GET', body, skipAuth = false, query } = options;

  const headers: Record<string, string> = {};
  if (body !== undefined) headers['Content-Type'] = 'application/json';

  if (!skipAuth) {
    const tokens = getStoredTokens();
    if (tokens) headers.Authorization = `Bearer ${tokens.accessToken}`;
  }

  const res = await fetch(buildUrl(path, query), {
    method,
    headers,
    body: body !== undefined ? JSON.stringify(body) : undefined,
  });

  if (res.status === 401 && !skipAuth && !isRetry) {
    try {
      await performRefresh();
    } catch {
      clearStoredTokens();
      emitSessionExpired();
      throw new ApiError('SESSION_EXPIRED', 'Your session has expired. Please log in again.', 401);
    }
    return request<T>(path, options, true);
  }

  if (!res.ok) {
    const { code, message } = await parseErrorBody(res);
    if (res.status === 401) {
      clearStoredTokens();
      emitSessionExpired();
    }
    throw new ApiError(code, message, res.status);
  }

  if (res.status === 204) return undefined as T;

  const text = await res.text();
  if (!text) return undefined as T;
  return JSON.parse(text) as T;
}

export const api = {
  auth: {
    register: (data: RegisterRequest) =>
      request<User>('/auth/register', { method: 'POST', body: data, skipAuth: true }),
    login: (data: LoginRequest) =>
      request<TokenPair>('/auth/login', { method: 'POST', body: data, skipAuth: true }),
    logout: () => request<void>('/auth/logout', { method: 'POST' }),
    mfaEnable: () => request<MfaEnableResponse>('/auth/mfa/enable', { method: 'POST' }),
    mfaVerify: (code: string) =>
      request<void>('/auth/mfa/verify', { method: 'POST', body: { code } }),
    forgotPassword: (email: string) =>
      request<void>('/auth/password/forgot', { method: 'POST', body: { email }, skipAuth: true }),
    resetPassword: (token: string, newPassword: string) =>
      request<void>('/auth/password/reset', {
        method: 'POST',
        body: { token, newPassword },
        skipAuth: true,
      }),
  },
  networks: {
    list: () => request<Network[]>('/networks'),
    create: (data: NetworkCreateRequest) =>
      request<Network>('/networks', { method: 'POST', body: data }),
    join: (data: JoinNetworkRequest) =>
      request<Network>('/networks/join', { method: 'POST', body: data }),
    get: (networkId: string) => request<Network>(`/networks/${networkId}`),
    update: (networkId: string, data: Partial<NetworkCreateRequest>) =>
      request<void>(`/networks/${networkId}`, { method: 'PATCH', body: data }),
    remove: (networkId: string) =>
      request<void>(`/networks/${networkId}`, { method: 'DELETE' }),
    rotateInvite: (networkId: string) =>
      request<RotateInviteResponse>(`/networks/${networkId}/invite`, { method: 'POST' }),
    members: (networkId: string) => request<Member[]>(`/networks/${networkId}/members`),
    updateMemberRole: (networkId: string, userId: string, role: NetworkRole) =>
      request<void>(`/networks/${networkId}/members/${userId}`, {
        method: 'PATCH',
        body: { role },
      }),
    removeMember: (networkId: string, userId: string) =>
      request<void>(`/networks/${networkId}/members/${userId}`, { method: 'DELETE' }),
    devices: (networkId: string) => request<Device[]>(`/networks/${networkId}/devices`),
    registerDevice: (networkId: string, data: DeviceRegisterRequest) =>
      request<Device>(`/networks/${networkId}/devices`, { method: 'POST', body: data }),
  },
  devices: {
    get: (deviceId: string) => request<Device>(`/devices/${deviceId}`),
    remove: (deviceId: string) => request<void>(`/devices/${deviceId}`, { method: 'DELETE' }),
    heartbeat: (deviceId: string) =>
      request<void>(`/devices/${deviceId}/heartbeat`, { method: 'POST' }),
    peers: (deviceId: string) => request<Device[]>(`/devices/${deviceId}/peers`),
  },
  logs: {
    audit: (networkId?: string) =>
      request<AuditLog[]>('/logs/audit', { query: { networkId } }),
    connections: (networkId?: string) =>
      request<ConnectionLog[]>('/logs/connections', { query: { networkId } }),
  },
  dashboard: {
    stats: () => request<DashboardStats>('/dashboard/stats'),
  },
};
