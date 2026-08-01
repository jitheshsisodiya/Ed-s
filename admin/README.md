# NexusVPN Admin Panel

React + TypeScript + Tailwind admin dashboard for the NexusVPN control
plane: dashboard analytics, network/device/member management, invite codes,
audit and connection logs, and account/MFA settings. Talks exclusively to
the REST API described in [`api/openapi.yaml`](../api/openapi.yaml).

## Stack

- [Vite](https://vitejs.dev/) + React 18 + TypeScript (strict mode)
- [react-router-dom](https://reactrouter.com/) v7 for routing/route guards
- [@tanstack/react-query](https://tanstack.com/query) for server-state
  caching, background refetch, and mutations
- Tailwind CSS (class-based dark mode, respects `prefers-color-scheme` by
  default with a manual toggle)
- [recharts](https://recharts.org/) for the dashboard charts
- [qrcode](https://www.npmjs.com/package/qrcode) for rendering MFA
  provisioning QR codes client-side

No component library / design system dependency — all UI primitives
(`Button`, `Input`, `Modal`, `DataTable`, `StatusBadge`, `RoleBadge`,
`Toast`, ...) live in `src/components/` and are hand-built with Tailwind.

## Getting started

```bash
cd admin
cp .env.example .env      # point VITE_API_BASE_URL at your backend
npm install
npm run dev                # http://localhost:5173
```

### Environment variables

| Variable | Description | Default |
|---|---|---|
| `VITE_API_BASE_URL` | Base URL of the REST API, including the `/api/v1` prefix. | `http://localhost:8080/api/v1` |

`VITE_API_BASE_URL` is baked into the JS bundle at **build time** (it's a
Vite env var). If you want the same built image to work across
environments, set it to a same-origin relative path like `/api/v1` and put
a reverse proxy in front that forwards `/api/` to the backend — this is
exactly what the shipped `Dockerfile` + `nginx.conf.template` do.

## Scripts

```bash
npm run dev       # start the Vite dev server
npm run build     # tsc -b (typecheck) && vite build -> dist/
npm run preview   # preview the production build locally
npm run lint      # eslint . (typescript-eslint, react-hooks, react-refresh)
```

## Architecture notes

- **Auth & tokens** (`src/lib/api.ts`, `src/lib/token-storage.ts`,
  `src/lib/auth-context.tsx`): the API client attaches the JWT access token
  as a `Bearer` header on every request. On a `401`, it makes exactly one
  `POST /auth/refresh` attempt with the stored refresh token and retries the
  original request once; if that also fails, tokens are cleared and an
  `auth-events` pub/sub notifies `AuthProvider`, which drops the session so
  `RequireAuth` redirects to `/login`.
- **Token storage tradeoff**: refresh tokens are kept in `localStorage`
  rather than an httpOnly cookie — documented in
  `src/lib/token-storage.ts`. A static SPA with no backend-for-frontend
  can't set httpOnly cookies from JS; if NexusVPN adds a BFF/reverse-proxy
  in front of the admin panel, migrate this to httpOnly cookies.
- **Current user identity**: the OpenAPI contract has no `GET /users/me`.
  The signed-in user's id/email/displayName/mfaEnabled are decoded from the
  JWT access token's claims (`src/lib/jwt.ts`) purely for display — the API
  itself is always the source of truth and enforces authorization
  server-side regardless of what the client believes.
- **Data fetching**: all server state goes through React Query hooks in
  `src/hooks/` (`useNetworks`, `useDevices`, `useLogs`, `useDashboard`).
  Mutations invalidate the relevant query keys on success. The dashboard
  and per-network device list poll on an interval (`refetchInterval`)
  instead of a push channel, matching the REST-only contract this admin
  panel is scoped to (the OpenAPI spec's `/ws` signaling channel is for
  clients, not the admin panel).
- **Cross-network device list** (`src/routes/devices/index.tsx`): the API
  only exposes devices scoped to a network
  (`GET /networks/{networkId}/devices`); there's no "all my devices"
  endpoint. `useAllDevices` fans out across every network the user belongs
  to and flattens the results client-side.
- **Known API gaps** (the admin UI adapts rather than fabricating
  endpoints):
  - No authenticated "change password" endpoint — Account Settings sends a
    password-reset email via `POST /auth/password/forgot` instead.
  - No "disable MFA" endpoint — only enable (`POST /auth/mfa/enable`) +
    verify (`POST /auth/mfa/verify`) exist, so the UI only offers enabling.
  - No `PATCH /users/me` — the profile section is read-only.
  - No network "broadcast toggle" field on the `Network`/
    `NetworkCreateRequest` schemas, so it was intentionally left out of
    Network Settings rather than added as a non-functional control.
- **Pagination**: none of the `GET` list endpoints in the OpenAPI spec take
  pagination parameters, so list/log endpoints are fetched in full and
  paginated client-side by the reusable `DataTable` component.

## Security notes

- **No source maps are shipped.** Anyone who can load the admin panel can
  load whatever sits beside it, and the annotated source of the console that
  manages every network is not something to hand out. Build with
  `npm run build -- --sourcemap` when you need them locally.
- Response headers are set by nginx from
  [`security-headers.conf`](./security-headers.conf): a strict CSP,
  `X-Frame-Options: DENY`, `X-Content-Type-Options: nosniff`, a
  `Referrer-Policy` and a `Permissions-Policy`.
- `npm audit --omit=dev --audit-level=high` runs in CI, so a vulnerable
  dependency that actually reaches the browser fails the build. Dev-only
  tooling is excluded deliberately: a vulnerable test runner cannot be
  reached by anyone loading the shipped bundle, and failing the build on one
  teaches people to ignore the job.
- One advisory is knowingly open: react-router's *RSC Mode CSRF Bypass*
  affects React Server Components with server actions. This panel is a
  client-side SPA using `<BrowserRouter>` — there is no RSC and no server
  action, so there is nothing for it to bypass. It has no fixed release yet;
  revisit when one lands.

## Docker / deployment

```bash
docker build -t nexusvpn-admin \
  --build-arg VITE_API_BASE_URL=/api/v1 \
  .
docker run -p 8080:8080 -e API_UPSTREAM=http://backend:8080 nexusvpn-admin
```

- Stage 1 builds the static bundle with Vite.
- Stage 2 serves it with `nginx:1.27-alpine`. `nginx.conf.template` is
  rendered by nginx's built-in `envsubst` entrypoint hook, substituting
  `${API_UPSTREAM}` so `/api/` is reverse-proxied to the backend control
  plane (also forwarding `Upgrade`/`Connection` headers for the `/api/v1/ws`
  signaling WebSocket).
- In `docker-compose`/Kubernetes, set `API_UPSTREAM` to the backend
  service's reachable address, e.g. `http://backend:8080`.
