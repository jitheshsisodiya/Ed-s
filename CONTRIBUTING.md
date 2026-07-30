# Contributing to NexusVPN

Thanks for considering a contribution. NexusVPN is organized as a
multi-component monorepo; see [`README.md`](README.md) for the layout and
[`docs/architecture.md`](docs/architecture.md) for the system design.

## Workflow

1. Fork/branch, make your change in the relevant component directory.
2. Add or update tests alongside your change — see each component's own
   `README.md` for how to run its test suite, or use the root `Makefile`
   (`make test`, `make lint`, `make build`).
3. Keep changes scoped to one component where possible; cross-cutting
   changes (e.g. an API contract change) should update `api/openapi.yaml`
   and/or `proto/coordination/v1/coordination.proto` first, since those are
   the source of truth other components are written against.
4. Open a pull request describing the change and how you tested it.

## Code standards

- Go: `gofmt`-clean, `go vet` clean, favor small interfaces at package
  boundaries (repository pattern for persistence, use-case/service layer
  for business logic, transport layer thin and framework-specific).
- TypeScript: strict mode, no `any` without justification, functional React
  components.
- Dart/Flutter: null-safe, `flutter analyze` clean.
- SQL migrations are additive/forward-only in spirit — write a new
  `NNNNNN_description.up.sql`/`.down.sql` pair rather than editing an
  already-merged migration.
- Commit messages: concise, imperative mood, explain *why* when not obvious.

## Reporting bugs / requesting features

Open a GitHub issue with reproduction steps (bugs) or motivation/use case
(features). For security vulnerabilities, see [`SECURITY.md`](SECURITY.md)
instead of a public issue.
