# georef chart

Deploys the IIIF image service: the front server, the conversion worker, and Cantaloupe as the
internal image server, sharing one ReadWriteMany volume. By default it also creates a
one-instance CloudNativePG cluster for the database, so the CNPG operator must be installed.

This chart carries generic defaults only. The deployment supplies everything specific to an
environment: `baseURL`, `image.tag`, storage classes and sizes, the ingress host, and the
secrets for login.

```sh
helm template georef deploy/chart \
  --set baseURL=https://georef.example.org --set image.tag=sha-0123abc
```

| Value | Meaning |
|---|---|
| `baseURL` | Required. Public URL, no trailing slash. IIIF URLs embed it permanently. |
| `image.tag` | Required. One tag for server and worker, e.g. `sha-<short>` from CI. |
| `auth.enabled` | Off by default, which means **no login at all**: anyone who reaches the server can upload, edit and delete sheets. When on, it needs `auth.existingSecret` (`GITHUB_CLIENT_ID`, `GITHUB_CLIENT_SECRET`, `SESSION_KEY`, optional `SERVICE_CREDENTIAL`) and `auth.allowedTeams`. |
| `storage.*` | The shared volume. `existingClaim` to bring your own. |
| `database.cnpg.enabled` | `false` to use `database.external` instead. |
| `cantaloupe.networkPolicy` | Only the server may reach Cantaloupe. Keep it on. |

The storage volume and the database cluster are annotated so that neither Helm uninstall nor
Argo CD pruning or deletion removes them. Remove them by hand when you mean to.

The server runs database migrations at start and has no health endpoint yet, so its probes
are TCP checks on port 8080. The worker may restart until the first migration has run.
