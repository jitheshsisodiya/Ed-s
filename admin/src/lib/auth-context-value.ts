import { createContext, useContext } from 'react';

import type { LoginRequest, RegisterRequest, TokenPair, User } from './types';

export interface SessionUser extends Pick<User, 'id' | 'email' | 'displayName' | 'mfaEnabled'> {
  status: User['status'];
}

export interface AuthContextValue {
  user: SessionUser | null;
  isAuthenticated: boolean;
  isBootstrapping: boolean;
  login: (data: LoginRequest) => Promise<TokenPair>;
  register: (data: RegisterRequest) => Promise<User>;
  logout: () => Promise<void>;
  /** Merge locally-known changes into the cached session user (e.g. after MFA enable). */
  patchUser: (patch: Partial<SessionUser>) => void;
}

export const AuthContext = createContext<AuthContextValue | null>(null);

/**
 * Access the current session. Kept in its own module (rather than alongside
 * AuthProvider) so the provider file only exports components, which is what
 * React Fast Refresh needs to reliably hot-reload it.
 */
export function useAuth(): AuthContextValue {
  const ctx = useContext(AuthContext);
  if (!ctx) throw new Error('useAuth must be used within an AuthProvider');
  return ctx;
}
