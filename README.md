# authentik-provisioner

An idempotent, config-driven provisioner that injects configuration into an
[authentik](https://goauthentik.io/) instance through its REST API. It runs as a
plain script or as a Kubernetes Job and is safe to run repeatedly: every object
is looked up by its natural key (slug / name / scope) and PATCHed when present,
POSTed otherwise.

It provisions:

- **OAuth2 / OIDC providers + applications** — confidential (client_secret) or
  public (PKCE). For confidential clients it writes the resulting
  `client_id` / `client_secret` into a Kubernetes secret. Application access can
  be restricted to a set of groups (`accessGroups`) via policy bindings, kept
  authoritative on each run.
- **OAuth social-login sources** (e.g. an upstream OIDC IdP), optionally exposed
  as a button on the login screen.
- **Brands (appearance / theme)** — per-domain branding: title, logo, favicon,
  flow background, custom CSS, default flows and landing application. Note that
  logo/favicon/flow-background are file paths served by authentik (not URLs); for
  a self-contained custom background, embed it from `customCSS` via a data-URI
  (`--ak-flow-background`). A complete example theme lives in
  [`examples/themes/cafe/`](./examples/themes/cafe).
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

## Kubernetes (Helm)

The chart is published as an OCI artifact on GHCR. It renders the ConfigMap
(your desired-state config), the Job, and the ServiceAccount/RBAC needed to
write client secrets. By default the Job is a Helm hook, so it re-runs on every
`helm upgrade`.

```bash
helm install authentik-provisioner \
  oci://ghcr.io/qjoly/charts/authentik-provisioner \
  --namespace authentik \
  --set authentik.url=http://authentik-server \
  --set authentik.token.existingSecret=authentik-bootstrap \
  --values my-values.yaml   # your `config:` desired state
```

The container image itself is `ghcr.io/qjoly/authentik-provisioner`. Both the
image and the chart are versioned by the same git tag.

Follow the run with:

```bash
kubectl -n authentik logs job/authentik-provisioner -f
```

Key values (see [`charts/authentik-provisioner/values.yaml`](./charts/authentik-provisioner/values.yaml)):

| Value                            | Description                                                        |
| -------------------------------- | ------------------------------------------------------------------ |
| `authentik.url`                  | Public authentik URL (without `/api/v3`).                          |
| `authentik.token.existingSecret` | Secret holding the API token (or set `authentik.token.value`).     |
| `config`                         | The desired-state config, rendered into a ConfigMap as config.yaml.|
| `existingConfigSecret.name`      | Mount the config from an existing Secret instead of a ConfigMap.   |
| `job.helmHook` / `job.argocdHook`| Re-run the Job via a Helm hook (default) or an Argo CD hook.        |
| `rbac.create`                    | Grant the secret write permission (needed for `secretName`).       |
| `extraEnv` / `extraEnvFrom`      | Inject values referenced as `${NAME}` in the config.               |

### Plain manifests

For a `kubectl`-only workflow, reference manifests live in [`deploy/`](./deploy)
(ServiceAccount/RBAC + a one-shot Job reading a ConfigMap named
`authentik-provisioner-config`).

Writing client secrets requires the `get/create/update` permission on `secrets`;
if no provider declares a `secretName` (or you pass `-no-secrets`), the
provisioner never touches the cluster and needs no RBAC.

## Layout

```
cmd/provisioner      entry point (flags, env, wiring)
internal/authentik   thin /api/v3 client (generic verbs + lookup helpers)
internal/config      YAML desired-state schema + loader
internal/provision   idempotent provisioning logic (providers, sources, users, groups)
internal/kube        Kubernetes secret writer (in-cluster or kubeconfig)
charts/              Helm chart (published to GHCR as an OCI artifact)
deploy/              plain kubectl manifests (alternative to the chart)
```
