# Diagrams

Standalone [Mermaid](https://mermaid.js.org) sources for the diagrams
embedded in [`../architecture.md`](../architecture.md). They live here as
individual files so they can be rendered on their own — into slides, an
architecture review, or PNG/SVG for a wiki.

| File | What it shows |
|---|---|
| `architecture.mmd` | Component overview: clients, control plane, relay fleet, data stores |
| `er-diagram.mmd` | Entity-relationship model, mirroring `db/migrations` |
| `sequence-connect.mmd` | Device join, hole punch, and relay fallback |
| `deployment.mmd` | Kubernetes topology, matching `deploy/k8s/` |

## Rendering

GitHub renders Mermaid natively in Markdown, so the copies in
`architecture.md` display without any tooling. To export images:

```bash
npm install -g @mermaid-js/mermaid-cli
mmdc -i architecture.mmd -o architecture.svg
```

These files are the same source as the fenced blocks in `architecture.md`;
if you change one, change the other.
