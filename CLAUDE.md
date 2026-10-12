# CLAUDE.md

## What this is

Portager is a Kubernetes operator that mirrors container images between OCI registries on a cron
schedule. Users declare an `ImageSync` resource — source images, destination registry, schedule, auth —
and the controller handles digest comparison, copying, validation gates and status reporting.

- API group `portager.portager.io/v1alpha1`, CRD `ImageSync` (namespaced)
- Repo `github.com/jarodr47/Portager`, Apache 2.0
- Current version: see [docs/STATUS.md](docs/STATUS.md)

## Hard constraints

- **Never edit generated files.** `api/v1alpha1/zz_generated.*`, `config/crd/bases/*`,
  `config/rbac/role.yaml`, `PROJECT`. Change the source markers and run `make manifests generate`.
- **`helm/portager/crds/` is a copy** of `config/crd/bases/`. `make manifests` syncs it; they must stay
  identical.
- **The Go version lives in three places** and they move together: the `go` directive in `go.mod`, the
  builder image in `Dockerfile`, and the pinned `golangci-lint` in `bin/`. A `k8s.io` minor bump can
  raise the directive on its own, and CI follows it via `go-version-file: go.mod`.
- **Chart version is not derived from the git tag.** `build-push.yml` runs `helm package`, which reads
  `Chart.yaml`. Tagging without bumping it republishes the previous chart version with new content,
  silently overwriting it in the OCI registry.
- **The e2e suite does not cover the sync path.** It boots the manager and scrapes metrics, nothing more.
  Any change to `sync/`, `tags/` or `verify/` needs hand-verification against a local registry — see
  [docs/DEVELOPMENT.md](docs/DEVELOPMENT.md).
- Don't refactor, rename or reformat anything outside the asked-for change.

## Workflow

1. Branch for every change. Never commit to `main`.
2. `make test` after each layer of change, not just at the end.
3. Before handing anything over: `make test`, `make test-e2e` (Kind), `make lint` (baseline 17 — don't
   add to it), `make helm-lint`, and a Trivy scan.
4. Leave changes uncommitted unless asked. Commit, push, merge and tagging are the maintainer's.
5. Run the `checkpoint` skill when context runs low, then `/clear`.

## Docs

| Doc | Read when | |
|---|---|---|
| [docs/STATUS.md](docs/STATUS.md) | planning work; checking what's built or broken | tracked |
| [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) | changing the reconciler, CRD, auth or sync path | tracked |
| [docs/DEVELOPMENT.md](docs/DEVELOPMENT.md) | setting up, running tests, or CI/toolchain breaks | tracked |
| [docs/CONFIGURATION.md](docs/CONFIGURATION.md) | Helm values, auth strategies, spec reference | tracked |
| [docs/DEPLOY_README.md](docs/DEPLOY_README.md) | deployment walkthroughs (EKS, GKE, Kind, kustomize) | tracked |
| `docs/ROADMAP.md` | picking up planned tiers | **local only** |
| `docs/SECURITY_POSTURE.md` | compliance, NIST mapping, federal questions | **local only** |
| `docs/CODE_WALKTHROUGH.md` | learning the codebase end to end | **local only** |
| `docs/spec.md` | historical design intent | **local only** |

Local-only docs are gitignored and exist on the maintainer's machine. Don't reference them from tracked
files — a fresh clone won't have them.
