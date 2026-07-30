// Tiny pub/sub so the framework-agnostic API client (lib/api.ts) can notify
// the React auth context (lib/auth-context.tsx) that the session died (both
// the access token AND the refresh-token retry failed) without importing
// React or react-router into the fetch layer.

type Listener = () => void;

const listeners = new Set<Listener>();

export function onSessionExpired(listener: Listener): () => void {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

export function emitSessionExpired(): void {
  for (const listener of listeners) listener();
}
