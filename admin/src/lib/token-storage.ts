// Token persistence for the SPA.
//
// TRADEOFF: a "real" web app would keep the refresh token in an httpOnly,
// Secure, SameSite=strict cookie so client-side JS (and therefore any XSS
// payload) can never read it. A pure static-file SPA talking to a REST API
// on a different origin/port cannot set httpOnly cookies from JS, and this
// admin panel has no backend-for-frontend of its own to broker that. We
// therefore fall back to localStorage, which is readable by any script
// running on the page. We mitigate this by: keeping access tokens short
// lived (server-issued, 15 min per docs/architecture.md), rotating refresh
// tokens on every use (server-side), and never rendering unsanitized
// third-party content in this app. If NexusVPN grows a BFF/reverse-proxy
// layer in front of the admin panel, migrate this to httpOnly cookies.

import type { TokenPair } from './types';

const STORAGE_KEY = 'nexusvpn.admin.tokens.v1';

export interface StoredTokens {
  accessToken: string;
  refreshToken: string;
  /** epoch ms when the access token is expected to expire */
  expiresAt: number;
}

export function getStoredTokens(): StoredTokens | null {
  const raw = localStorage.getItem(STORAGE_KEY);
  if (!raw) return null;
  try {
    return JSON.parse(raw) as StoredTokens;
  } catch {
    return null;
  }
}

export function setStoredTokens(tokens: TokenPair): StoredTokens {
  const stored: StoredTokens = {
    accessToken: tokens.accessToken,
    refreshToken: tokens.refreshToken,
    expiresAt: Date.now() + tokens.expiresIn * 1000,
  };
  localStorage.setItem(STORAGE_KEY, JSON.stringify(stored));
  return stored;
}

export function clearStoredTokens(): void {
  localStorage.removeItem(STORAGE_KEY);
}
