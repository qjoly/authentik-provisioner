# authentik-provisioner

An idempotent, config-driven provisioner that injects configuration into an
[authentik](https://goauthentik.io/) instance through its REST API. It runs as a
plain script or as a Kubernetes Job and is safe to run repeatedly: every object
is looked up by its natural key (slug / name / scope) and PATCHed when present,
POSTed otherwise.

It provisions:

- **OAuth2 / OIDC providers + applications** — confidential (client_secret) or
  public (PKCE). For confidential clients it writes the resulting
  `client_id` / `client_secret` into a Kubernetes secret.
- **OAuth social-login sources** (e.g. an upstream OIDC IdP), optionally exposed
  as a button on the login screen.
- **Users** — pre-created internal users so group membership works before their
  first login.
- **Groups** — with an authoritative member list, and optional pruning of
  managed groups that disappear from the config.
- **Bootstrap** — (re)setting the first user's (`akadmin`) password and email.

## Usage

```bash
export AUTHENTIK_URL=https://auth.example.com   # public URL, without /api/v3
export AUTHENTIK_TOKEN=...                       # admin service-account token

go run ./cmd/provisioner -config config.yaml
# or, once built:
provisioner -config config.yaml
```

### Flags

| Flag          | Default        | Description                                                   |
| ------------- | -------------- | ------------------------------------------------------------- |
| `-config`     | `config.yaml`  | Path to the YAML desired-state config.                        |
| `-dry-run`    | `false`        | Log intended changes without calling the API or the cluster. |
| `-no-secrets` | `false`        | Never write Kubernetes secrets, even if providers ask for it. |
| `-wait`       | `true`         | Wait for the authentik API to be ready before provisioning.  |

### Configuration

See [`config.example.yaml`](./config.example.yaml) for a fully commented
example. `${ENV}` references inside the file are expanded from the process
environment at load time, so redirect URIs, client secrets and passwords can be
injected without hardcoding them.

## Kubernetes

Build and push the image, then apply the manifests in [`deploy/`](./deploy):

```bash
docker build -t ghcr.io/qjoly/authentik-provisioner:latest .

kubectl apply -f deploy/rbac.yaml
kubectl create configmap authentik-provisioner-config \
  --from-file=config.yaml=config.yaml -n authentik
kubectl apply -f deploy/job.yaml
```

The Job runs the provisioner once; wire it as an ArgoCD `PostSync` hook or a
Helm hook to re-run it on every sync. Writing client secrets requires the
`get/create/update` permission on `secrets` granted by `deploy/rbac.yaml`; if no
provider declares a `secretName` (or you pass `-no-secrets`), the provisioner
never touches the cluster and needs no RBAC.

## Layout

```
cmd/provisioner      entry point (flags, env, wiring)
internal/authentik   thin /api/v3 client (generic verbs + lookup helpers)
internal/config      YAML desired-state schema + loader
internal/provision   idempotent provisioning logic (providers, sources, users, groups)
internal/kube        Kubernetes secret writer (in-cluster or kubeconfig)
```
