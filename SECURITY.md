# Security Policy

## Reporting a vulnerability

Please **do not** open a public GitHub issue for security vulnerabilities.

Instead, use GitHub's private vulnerability reporting
(Security → Report a vulnerability on this repository), or email the
maintainers listed in the repository's contact information. Include:

- A description of the vulnerability and its impact
- Steps to reproduce (proof-of-concept code/config welcome)
- Affected component(s) (backend, relay, client, desktop, mobile, admin)

We aim to acknowledge reports within 5 business days.

## Scope

In scope: the control plane (`backend/`), relay (`relay/`), client core
(`client/`), desktop app (`desktop/`), mobile app (`mobile/`), admin panel
(`admin/`), and deployment manifests (`deploy/`).

## Supported versions

Only the latest release on the `main` branch receives security fixes until
NexusVPN reaches a stable 1.0 release, after which a support policy will be
published here.

## Design notes relevant to security review

See [`docs/architecture.md`](docs/architecture.md#security-model) for the
threat model summary: transport security (TLS 1.3 for control plane,
WireGuard/Curve25519/ChaCha20-Poly1305 for the data plane), token lifecycle
(short-lived JWT access tokens, rotating revocable refresh tokens), device
key custody (private keys never leave the device), and RBAC enforcement.
