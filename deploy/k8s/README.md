# NexusVPN Kubernetes manifests

Apply in order (numeric prefixes control the order):

```bash
kubectl apply -f deploy/k8s/00-namespace.yaml
cp deploy/k8s/01-secrets.example.yaml deploy/k8s/01-secrets.yaml
# edit deploy/k8s/01-secrets.yaml with real values, then:
kubectl apply -f deploy/k8s/01-secrets.yaml
kubectl apply -f deploy/k8s/10-postgres.yaml
kubectl apply -f deploy/k8s/11-redis.yaml
kubectl apply -f deploy/k8s/20-backend.yaml
kubectl apply -f deploy/k8s/21-relay.yaml
kubectl apply -f deploy/k8s/22-admin.yaml
kubectl apply -f deploy/k8s/30-ingress.yaml
kubectl apply -f deploy/k8s/40-networkpolicy.yaml
```

Or simply: `kubectl apply -f deploy/k8s/`

## Notes

- `01-secrets.yaml` is gitignored — never commit real secrets. In production,
  prefer External Secrets Operator / Sealed Secrets / your cloud KMS instead
  of plain `Secret` objects.
- `10-postgres.yaml` / `11-redis.yaml` are single-replica StatefulSets meant
  for demo/small deployments. For production, use a managed database or an
  HA operator (CloudNativePG, Zalando Postgres Operator, Redis Enterprise/
  Bitnami Redis Cluster chart) and point `DATABASE_URL`/`REDIS_URL` at it
  instead.
- `21-relay.yaml` runs as a `DaemonSet` with `hostNetwork: true` on nodes
  labeled `nexusvpn.io/relay-node=true`, since relay UDP sessions need a
  stable per-node routable endpoint (a ClusterIP Service would break UDP
  session affinity). Label relay nodes with:
  `kubectl label node <node> nexusvpn.io/relay-node=true` and taint them so
  only relay pods schedule there.
- The backend `Deployment` is horizontally scaled via `HorizontalPodAutoscaler`
  (3-10 replicas on CPU/memory) and is safe to scale because all mutable
  session state (presence, pubsub, ICE candidate rendezvous) lives in Redis,
  not in-process.
- Ingress assumes `ingress-nginx` + `cert-manager` are already installed in
  the cluster. Swap annotations if you use a different ingress controller.
