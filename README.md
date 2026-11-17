# gitea-operator

A Kubernetes-Operator to deploy and Manage [Gitea](https://github.com/go-gitea/gitea)

## Description

deploy a gitea instance and manage users, ssh keys, organizations, organization teams, repos, and authentication sources.

## Authentication sources (`Auth`)

An `Auth` resource manages an OAuth2/OpenID Connect authentication source
(Site Administration > Authentication Sources). See
[`config/samples/v1_auth.yaml`](config/samples/v1_auth.yaml) for a commented
example.

Gitea has no REST API for authentication sources, so the operator runs
`gitea admin auth add-oauth` / `update-oauth` inside a ready Gitea pod (which is
why the operator's role includes `pods/exec`).

- **Matching:** the source is found by `spec.name`. If a source with that name
  already exists, it is adopted and updated in place, keeping its ID. The ID is
  reported in `status.sourceId`, which is what `User.spec.sourceId` refers to.
- **Credentials:** `clientID` and `clientSecret` are Secret key references and
  are never stored in the resource.
- **Drift:** every flag is passed on each update, so changes made in the web UI
  are overwritten. The operator re-checks every 10 minutes and skips the update
  when nothing changed. Secrets are not watched, so a rotated client secret is
  picked up within 10 minutes (or edit the `Auth` to apply it immediately).
- **Deletion:** with `deletionPolicy: Retain` (the default), deleting the `Auth`
  leaves the source in Gitea. With `Delete`, the operator deletes it; Gitea
  refuses if users are still bound to the source.
- **Not settable:** Gitea's CLI has no flag for the external ID claim, so it
  keeps whatever value it has.

### Group claims to teams

`groupClaimName` names the claim that lists the user's groups (for IBM Security
Verify, `blue_groups`). `groupTeamMap` maps each group to the org teams its
members join:

```yaml
groupClaimName: blue_groups
groupTeamMap:
  ITZ_ACL_development:      # group name, as it appears in the claim
    itz: [Development]      # org: [teams]
    itz-content: [Development]
groupTeamMapRemoval: true   # also remove users who lost the group
```

Gitea applies the map **when a user signs in**, not when the map changes.
Org and team names are matched case-insensitively. If a mapped org or team
doesn't exist, Gitea logs a warning and skips it; sign-in still works. With
`groupTeamMapRemoval: true`, a user who no longer has a group is removed from
that group's teams at their next sign-in. Only teams in the map are affected.

## Team membership (`memberPolicy`)

Each team in an `Org` has a `memberPolicy` that controls what the operator does
with the team's `members` list:

| `memberPolicy` | Adds listed members | Removes unlisted members |
|---|---|---|
| `Exact` | yes | yes |
| `Additive` | yes | no |
| `Ignore` | no | no |
| *(unset)* | `Ignore` if the team is group-mapped, otherwise `Exact` | |

When `memberPolicy` is unset, a team counts as **group-mapped** if any `Auth`
for the same Gitea instance (and not being deleted) lists it in
`groupTeamMap`. The identity provider owns that team's membership, so the
operator leaves it alone; otherwise the operator would remove the users Gitea
added at sign-in. This applies whether or not `groupTeamMapRemoval` is set.
The Org controller watches `Auth` resources, so editing a `groupTeamMap` takes
effect right away.

Set `memberPolicy` explicitly to override this. For example, use `Additive` to
guarantee a service account is in a group-mapped team while still letting
group sync add people:

```yaml
teams:
- name: Development      # group-mapped: membership comes from the IdP
  permission: write
- name: Owners
  permission: owner
  memberPolicy: Additive # group-mapped, but always keep this account
  members: [ci-bot]
- name: automation       # not mapped: members are exactly this list
  members: [ci-bot]
```

> **Upgrading:** older CRDs defaulted `memberPolicy` to `Exact`. Two things
> keep that behaviour after an upgrade:
>
> 1. **The default stays in the CRD.** `kubectl apply`, and the operator's own
>    server-side apply of its CRDs at startup, don't remove a field that another
>    field manager set, so the old `default: Exact` survives. Replace the CRD
>    instead:
>    `kubectl replace -f config/crd/bases/hyperspike.io_orgs.yaml`.
>    Check with
>    `kubectl get crd orgs.hyperspike.io -o yaml | grep -A3 memberPolicy:`.
> 2. **The value is saved on existing teams.** Every team created under the old
>    CRD has `memberPolicy: Exact` stored, which overrides the group-mapped
>    default. Remove the field from those teams (or recreate the Org).

## Operator configuration

Environment variables on the manager deployment:

| Variable | Default | Purpose |
|---|---|---|
| `WATCH_NAMESPACES` | `default,cert-manager` | Comma-separated namespaces the operator watches. Gitea resources outside these are ignored. |
| `SPOON_IMAGE` | `ghcr.io/hyperspike/gitea-spoon:v0.0.5` | Image for the gitea-spoon sidecar in each Gitea pod. |

## Getting Started

### Prerequisites
- go version v1.22.0+
- docker version 17.03+.
- kubectl version v1.11.3+.
- Access to a Kubernetes v1.11.3+ cluster or Minikube.

### To Deploy on the cluster
**Build and push your image to the location specified by `IMG`:**

```sh
make docker-build docker-push IMG=<some-registry>/gitea-operator:tag
```

**NOTE:** This image ought to be published in the personal registry you specified.
And it is required to have access to pull the image from the working environment.
Make sure you have the proper permission to the registry if the above commands don’t work.

**Install the CRDs into the cluster:**

```sh
make install
```

**Deploy the Manager to the cluster with the image specified by `IMG`:**

```sh
make deploy IMG=<some-registry>/gitea-operator:tag
```

> **NOTE**: If you encounter RBAC errors, you may need to grant yourself cluster-admin
privileges or be logged in as admin.

**Create instances of your solution**
You can apply the samples (examples) from the config/sample:

```sh
kubectl apply -k config/samples/
```

>**NOTE**: Ensure that the samples has default values to test it out.

### To Uninstall
**Delete the instances (CRs) from the cluster:**

```sh
kubectl delete -k config/samples/
```

**Delete the APIs(CRDs) from the cluster:**

```sh
make uninstall
```

**UnDeploy the controller from the cluster:**

```sh
make undeploy
```

## Project Distribution

Following are the steps to build the installer and distribute this project to users.

1. Build the installer for the image built and published in the registry:

```sh
make build-installer IMG=<some-registry>/gitea-operator:tag
```

NOTE: The makefile target mentioned above generates an 'install.yaml'
file in the dist directory. This file contains all the resources built
with Kustomize, which are necessary to install this project without
its dependencies.

2. Using the installer

Users can just run kubectl apply -f <URL for YAML BUNDLE> to install the project, i.e.:

```sh
kubectl apply -f https://raw.githubusercontent.com/<org>/gitea-operator/<tag or branch>/dist/install.yaml
```

## Contributing
// TODO(user): Add detailed information on how you would like others to contribute to this project

**NOTE:** Run `make help` for more information on all potential `make` targets

More information can be found via the [Kubebuilder Documentation](https://book.kubebuilder.io/introduction.html)

```sh
make minikube
```

```sh
make port-forward
```

```sh
make tunnel
```

```sh
TAG=latest; make docker-build IMG=localhost:5000/controller:$TAG; docker push localhost:5000/controller:$TAG ; make IMG=localhost:5000/controller:$TAG build-installer  ; kubectl apply -f dist/install.yaml
```

## Security

To validate a container image, you can use the following command:

```sh
LATEST=$(curl -s https://api.github.com/repos/hyperspike/gitea-operator/releases/latest | jq -cr .tag_name)
cosign verify ghcr.io/hyperspike/gitea-operator:$LATEST  --certificate-oidc-issuer https://token.actions.githubusercontent.com --certificate-identity https://github.com/hyperspike/gitea-operator/.github/workflows/image.yaml@refs/tags/$LATEST
```

## License

Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

