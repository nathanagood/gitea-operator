# AGENTS.md

Guidance for AI coding agents working in this repository. For the project
architecture, CRD reference and build commands, see [`GEMINI.md`](GEMINI.md) and
[`.github/copilot-instructions.md`](.github/copilot-instructions.md). This file
records the current decisions and the in-flight work.

Line numbers below are from commit `5c09f3e` and will drift. Search for the
symbol names rather than trusting the numbers.

---

## Current task: remove the `gitea-spoon` dependency

**Decision (made by the repo owner):** `gitea-spoon` is being removed
completely. The operator must do all the work that is necessary itself. Do not
publish, build, version or document a `gitea-spoon` image for this repository.
The sibling repo at `/home/ngood/Code/nathanagood/gitea-spoon` is not part of
the plan and needs no changes.

### What gitea-spoon is

A sidecar container the operator injects into every Gitea pod
(`gitea-spoon`, image from `imageSpoon()`). Its README ("a sidecar controller
that manages Gitea Administrative tasks") is vague. The code does two things:

1. **Reconciles the Gitea admin password.**
   [`internal/local/controller.go`](../gitea-spoon/internal/local/controller.go)
   loops every 7 seconds:
   - reads `GITEA_ADMIN_USERNAME` / `GITEA_ADMIN_PASSWORD`, which the operator
     injects from the `<gitea>-admin` Secret;
   - calls `GET http://localhost:3000/api/v1/user` with basic auth;
   - if that is not a 200, it opens Gitea's database directly, using the
     connection settings in `/data/gitea/conf/app.ini`, sets the password and
     clears `must_change_password`.
2. **An `Auth` controller.** This duplicates the operator's own
   `AuthReconciler`, which has already replaced it. Upstream spoon releases up
   to v0.0.5 still reconcile `Auth` and conflict with the operator. This is
   noted in the comment on `defaultSpoonImage`.

### Why it exists (the root cause)

The operator depends on the admin password being correct. `adminToken` in
[`internal/controller/gitea_controller.go`](internal/controller/gitea_controller.go)
logs in with basic auth (`<gitea>-admin` Secret) to mint the admin API token.

The init script `configure_admin_user` (embedded in the `<gitea>-init` Secret as `configure_gitea.sh`)
has a quoting bug in the branch that runs when the admin user already exists:

```sh
gitea admin user change-password --username "${GITEA_ADMIN_USERNAME}" --password "${GITEA_ADMIN_PASSWORD} --must-change-password=false"
```

The closing quote comes after the flag, so the flag becomes part of the
password. On every pod start after the first, the admin password is set to
`<password> --must-change-password=false`. The first start uses the `create`
branch, which is written correctly. The sidecar repairs the damage within about
7 seconds. The commit that introduced the flag text is `3f952c2`
("fix pod ready bug and password reset bug").

The admin Secret is created once and never rewritten (`upsertSecret(..., once=true)`),
so no operator feature needs live password syncing apart from this bug.

> **Status of this explanation:** inferred from reading the code. It has **not**
> been reproduced in a cluster. In particular, whether `gitea admin user
> change-password` defaults `--must-change-password` to `true` has not been
> checked against the Gitea version in use. Reproduce it before relying on it
> (see "Verification").

### Why it is a bad design

- It writes to Gitea's database directly, bypassing Gitea, and links Gitea's
  internal Go packages (`code.gitea.io/gitea/models/...`).
- It is version-coupled: spoon is compiled against `code.gitea.io/gitea v1.26.4`,
  but the Gitea image is whatever the user sets in `spec.image`. A schema or
  password-hash difference between the two is a latent failure.
- It needs database credentials via `app.ini` on the shared volume.
- It polls forever and authenticates against Gitea every 7 seconds.
- It costs a container, an image, a repo and a CI pipeline to compensate for one
  misplaced quote.

### Target design

1. Fix the init script so the existing-admin branch uses Gitea's own CLI, at the
   matching Gitea version, in the init container that already runs on each pod
   start:

   ```sh
   gitea admin user change-password --username "${GITEA_ADMIN_USERNAME}" --password "${GITEA_ADMIN_PASSWORD}" --must-change-password=false
   ```

2. Remove the sidecar and everything that only exists for it.

Out of scope unless the owner asks: live drift repair (for example if someone
changes the admin password in the web UI). If it is ever wanted, do it in the
operator, using the Gitea admin API or `pods/exec` (the operator already has
`pods/exec` for `Auth`). Do not reintroduce direct database access.

### Status: done on branch `fix/remove-gitea-spoon` (not yet committed)

- [x] Fixed the `change-password` quote. The script is now the package constant
      `configureGiteaScript` in `gitea_controller.go`.
- [x] Removed the `gitea-spoon` container, both `Containers[1]` env lines,
      `defaultSpoonImage`, `imageSpoon()` and the `SPOON_IMAGE` README row.
      `GEMINI.md` and `.github/copilot-instructions.md` never mentioned spoon.
- [x] Nothing else referenced ports 8081/8443 in the Gitea pod.
- [x] Kept the per-instance `Role`/`RoleBinding`. Phase 3 may use them to grant
      an SCC to the Gitea ServiceAccount.
- [x] Test: `configure_gitea_script_test.go` runs the script under bash with a
      fake `gitea` and checks the exact `change-password` / `create` arguments.
      It fails against the old script.
- [ ] Upgrade: the StatefulSet is create-only, so existing instances keep the
      sidecar. Until Phase 2 makes it update in place, remove it by hand:
      `oc patch sts <gitea> --type=json -p '[{"op":"remove","path":"/spec/template/spec/containers/1"}]'`
      (check that `containers[1]` is `gitea-spoon` first).

### Verification results (2026-11-24, CRC, Gitea 1.27.3)

- `gitea admin user change-password --help` (1.27.3): `--must-change-password`
  defaults to true, so the bug also forced a password change.
- Old operator, sidecar removed, pod restarted: admin `GET /api/v1/user` -> 401,
  operator errors "You must change your password". **Root cause confirmed.**
- New operator (`gitea-operator:no-spoon-20261124125620` in the CRC registry,
  `SPOON_IMAGE` removed from the deployment), two restarts: 200 each time,
  token works, errors stop.
- `make test`: the 6 scaffold envtest specs fail on `main` too (pre-existing).
- `make lint` fails under Go 1.27 (golangci-lint v2.12.2 can't read 1.27 export
  data). Run it as
  `GOTOOLCHAIN=go1.26.3 go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2 run ./...`;
  9 pre-existing `goconst` findings remain.

### Pre-existing problems seen on CRC (all resolved in 2b/3)

- Events forbidden for `events.k8s.io`: fixed (RBAC).
- Repo controller re-creating existing repos: fixed.
- "tls: true but Gitea serves HTTP": the live CR on CRC actually has
  `tls: false`; no ClusterIssuer exists there. Not an operator bug.
- No readiness probe on `gitea`: added.

## Overall plan (agreed with the owner 2026-11-24)

Phase 4 will be a **fresh install** on CRC (delete `gitea-test`,
`gitea-operator-system`, and the CRDs; the CRC VM itself need not be rebuilt).

1. **Remove gitea-spoon.** Done (see above).
2. **2a, all checks passing.** Done. The Makefile sets `GOTOOLCHAIN` from
   `go.mod`; CI uses `go-version-file: go.mod` and runs `make test lint` on PRs.
   The 6 scaffold envtest specs were deleted; `goconst` is off for tests.
   Verify envtest really runs with `GOFLAGS=-v make test` (expect "Ran N of N").
   An invalid CRD makes BeforeSuite fail and skips every spec.
3. **2b, GitOps safety.** Done, verified on CRC:
   - Repo `spec.name` (Gitea name; defaults to metadata.name), `spec.archived`;
     CEL rules make spec.name and spec.org.name immutable. Convention:
     metadata.name `<org>-<repo>` lowercased. Deleting a Repo deletes it in
     Gitea; if the Org/Gitea resource is already gone the Repo is released.
   - Repo create-if-missing, then edits only changed description/private/archived;
     resync every 10 minutes. User-owned repos now return an error (they never worked).
   - `spec.clusterDomain` is used if set, else detected; never written back.
   - StatefulSet and Ingress update in place, keyed on the
     `hyperspike.io/applied-hash` annotation; foreign pod-template annotations
     (Auth restarts, rollout restarts) are kept. `env()`/`volumes()` are sorted
     so the hash is stable.
   - RBAC: `events.k8s.io` events; update/patch on StatefulSets and Ingresses.
4. **3, OpenShift.** Operator side done, verified on CRC:
   - `spec.ingress.className: openshift-default` -> no TLS section plus
     `route.openshift.io/termination: edge` (unless set, and only with
     `tls: false`) -> edge Route with the router's certificate.
   - `spec.rootless: true` with a `-rootless` image -> runs under
     `restricted-v2` with no SCC grant (non-root, drop ALL, seccomp default;
     init scripts skip chown and set `RUN_USER=$(id -un)`; built-in SSH).
   - Readiness probe on `/api/healthz`.
   - Recommended on OpenShift: `tls: false` (router terminates TLS),
     `rootless: true`. Re-encrypt to an HTTPS Gitea is untested.
   - Still to do: cut a release (owner pushes a signed tag) so ArgoCD can
     install a fixed version.
5. **4, ArgoCD** in `gitea-yaml-configuration`: app-of-apps with sync waves that
   installs cert-manager and CloudNativePG (OLM), the operator, then the
   instance and its resources. Ship the OIDC Secret with placeholder values and
   make ArgoCD ignore its data so real values are not reverted. README covers
   naming, archive vs delete, and the secret. Install OpenShift GitOps on CRC.
   Known data issue: Org team `members` who have never signed in via SSO don't
   exist in a fresh Gitea, so the Org controller logs "not found" for them.

### CRC state left by this work (2026-11-24)

- Operator image `gitea-operator:<tag>` in the CRC registry (latest tag in the
  scratchpad `tag` file of the session); `SPOON_IMAGE` removed;
  `WATCH_NAMESPACES=default,cert-manager,gitea-test,claude-rootless-test`.
- `gitea-test/git-techzone`: spoon removed, `spec.ingress.className:
  openshift-default` (patched live), still rootful under a hand-granted
  `privileged` SCC.
- `claude-rootless-test/gitea-rootless`: rootless test instance. Delete the
  namespace when done.

---

## Release pipeline (verified working)

The CI is already configured to build and push to `ghcr.io`. It was tested end to
end with the throwaway tag `v0.0.1-rc.1`.

| Workflow | Trigger | What it does |
|---|---|---|
| [`image.yaml`](.github/workflows/image.yaml) | any tag (`'*'`) | `make V=1`, build and push `ghcr.io/<repo>:<tag>`, cosign sign, Trivy scan |
| [`publish.yaml`](.github/workflows/publish.yaml) | tags `v*` | creates a GitHub release and attaches `dist/install.yaml` |
| [`build.yaml`](.github/workflows/build.yaml) | pull requests | `make V=1` only |

Facts established during testing:

- Image: `ghcr.io/nathanagood/gitea-operator`. A tag push produces the tag itself
  plus `latest`, and a `sha256-...` signature tag.
- The package is public. An unauthenticated `podman pull` works.
- The release's `install.yaml` references the matching tag.
- **A fork's Actions are disabled until enabled in the repo settings.** A tag
  pushed before that is silently ignored. Re-push it after enabling.
- A tag that does not start with `v` runs only `image.yaml`. Use that for
  image-only tests, to avoid creating a GitHub release.
- `${{ env.RELEASE_VERSION }}` in `image.yaml` is never defined and expands to an
  empty string. It is harmless but dead; it is appended to the image name with no
  separator, so setting it would change the image name, not the tag. Remove it
  when convenient.
- The dependency pins (`go 1.26.3` in `go.mod`, `go-version: 1.26` in the
  workflows) agree.

### Test artifacts still to clean up

- Tag `v0.0.1-rc.1` (remote and local).
- The GitHub release `v0.0.1-rc.1`.
- The `v0.0.1-rc.1`, `latest` and signature package versions under GitHub
  Packages.
- The local image `ghcr.io/nathanagood/gitea-operator:v0.0.1-rc.1` in podman.

---

## Operational notes for agents

- **Build and run on CRC with `make crc-deploy`** (push the image and point the
  running operator at it) or `make crc-install` (apply namespace, CRDs, RBAC and
  deployment; use after RBAC/CRD changes or on a fresh cluster). Logic is in
  `hack/crc.sh`; it refuses to run unless `oc` points at
  `https://api.crc.testing:6443`. Set `CRC_WATCH_NAMESPACES` for crc-install.

- **Git signing is enforced** (`commit.gpgsign` and `tag.gpgsign` are `true`).
  Signing needs an interactive passphrase, which a non-interactive shell cannot
  supply. Running `git tag <name>` without `-m` also opens an editor and hangs.
  For throwaway tags use `GIT_EDITOR=true git tag --no-sign <name>`. Create real
  release tags from the owner's own terminal.
- **`gh auth status` can hang.** `gh run list`, `gh release view` and `gh api`
  work. Wrap `gh` calls in `timeout`, and quote URLs containing `?` (zsh).
- **Do not use `pkill -f` with a pattern that appears in your own command line.**
  It kills the shell running it. Use `ps | grep` with a bracket trick, such as
  `[v]im`.
- `git status`, `git ls-remote` and similar commands can be slow. Give them a
  generous timeout.
- The `manager` binary in the repo root is a gitignored build artifact. The
  `Dockerfile` copies it in, so `make` must run before `docker build`.
- `dist/install.yaml` in the repo points at `localhost:5000/gitea-operator:1`. It
  is a local development artifact. Regenerate it with
  `make build-installer IMG=...` before using it.
