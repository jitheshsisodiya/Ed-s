// Minimal JWT payload decoder. NexusVPN's OpenAPI contract has no
// `GET /users/me` endpoint, so the admin panel derives the signed-in user's
// identity from the claims embedded in the JWT access token returned by
// /auth/login and /auth/refresh. This does NOT verify the token's
// signature (the browser has no way to do that trustworthily anyway,
// nor should it need to -- the API is the source of truth and simply
// rejects/401s an invalid or expired token on the next request); it is
// purely used to render "who am I" in the UI.

export interface JwtClaims {
  sub?: string;
  userId?: string;
  id?: string;
  email?: string;
  displayName?: string;
  name?: string;
  mfaEnabled?: boolean;
  exp?: number;
  iat?: number;
}

function base64UrlDecode(segment: string): string {
  const padded = segment.replace(/-/g, '+').replace(/_/g, '/').padEnd(
    segment.length + ((4 - (segment.length % 4)) % 4),
    '=',
  );
  return decodeURIComponent(
    atob(padded)
      .split('')
      .map((c) => '%' + c.charCodeAt(0).toString(16).padStart(2, '0'))
      .join(''),
  );
}

export function decodeJwtPayload(token: string): JwtClaims | null {
  const parts = token.split('.');
  if (parts.length !== 3) return null;
  try {
    return JSON.parse(base64UrlDecode(parts[1])) as JwtClaims;
  } catch {
    return null;
  }
}
