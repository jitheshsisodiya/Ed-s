import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from 'react';
import { api, ApiError } from './api';
import { clearStoredTokens, getStoredTokens, setStoredTokens } from './token-storage';
import { onSessionExpired } from './auth-events';
import { decodeJwtPayload } from './jwt';
import type { LoginRequest, RegisterRequest, TokenPair, User } from './types';

interface SessionUser extends Pick<User, 'id' | 'email' | 'displayName' | 'mfaEnabled'> {
  status: User['status'];
}

function userFromAccessToken(accessToken: string): SessionUser | null {
  const claims = decodeJwtPayload(accessToken);
  if (!claims) return null;
  const id = claims.sub ?? claims.userId ?? claims.id ?? '';
  if (!id) return null;
  return {
    id,
    email: claims.email ?? '',
    displayName: claims.displayName ?? claims.name ?? claims.email ?? 'Account',
    mfaEnabled: claims.mfaEnabled ?? false,
    status: 'active',
  };
}

interface AuthContextValue {
  user: SessionUser | null;
  isAuthenticated: boolean;
  isBootstrapping: boolean;
  login: (data: LoginRequest) => Promise<TokenPair>;
  register: (data: RegisterRequest) => Promise<User>;
  logout: () => Promise<void>;
  /** Merge locally-known changes into the cached session user (e.g. after MFA enable). */
  patchUser: (patch: Partial<SessionUser>) => void;
}

const AuthContext = createContext<AuthContextValue | null>(null);

export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<SessionUser | null>(null);
  const [isBootstrapping, setIsBootstrapping] = useState(true);

  useEffect(() => {
    const stored = getStoredTokens();
    if (stored) {
      setUser(userFromAccessToken(stored.accessToken));
    }
    setIsBootstrapping(false);
  }, []);

  useEffect(() => {
    return onSessionExpired(() => {
      setUser(null);
    });
  }, []);

  const login = useCallback(async (data: LoginRequest) => {
    const pair = await api.auth.login(data);
    if (pair.mfaRequired) {
      // Server withheld/limited the tokens pending an MFA code; do not
      // persist a session yet. The login route re-submits with mfaCode.
      return pair;
    }
    setStoredTokens(pair);
    setUser(userFromAccessToken(pair.accessToken));
    return pair;
  }, []);

  const register = useCallback(async (data: RegisterRequest) => {
    return api.auth.register(data);
  }, []);

  const logout = useCallback(async () => {
    try {
      if (getStoredTokens()) {
        await api.auth.logout();
      }
    } catch (err) {
      // Best-effort: even if the server call fails (network error, already
      // expired token) we still clear the local session below.
      if (!(err instanceof ApiError)) {
        console.warn('Logout request failed', err);
      }
    } finally {
      clearStoredTokens();
      setUser(null);
    }
  }, []);

  const patchUser = useCallback((patch: Partial<SessionUser>) => {
    setUser((prev) => (prev ? { ...prev, ...patch } : prev));
  }, []);

  const value = useMemo<AuthContextValue>(
    () => ({
      user,
      isAuthenticated: user !== null,
      isBootstrapping,
      login,
      register,
      logout,
      patchUser,
    }),
    [user, isBootstrapping, login, register, logout, patchUser],
  );

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

export function useAuth(): AuthContextValue {
  const ctx = useContext(AuthContext);
  if (!ctx) throw new Error('useAuth must be used within an AuthProvider');
  return ctx;
}

export type { SessionUser };
