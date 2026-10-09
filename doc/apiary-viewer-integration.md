# Apiary Viewer queries

orcli exposes `apiary_query` when the configuration contains an Apiary REST shim
URL and an existing Viewer API token:

```json
{
  "apiary": {
    "base_url": "https://apiary.example.invalid",
    "viewer_token": "apk_…"
  }
}
```

The token must already exist and have the Apiary Viewer role. orcli does not
create keys or change roles. Configure the REST shim with TLS appropriate to
your deployment; do not send a Viewer token over an untrusted network.

The model can query `status`, `health`, `vms`, `vm`, `jails`, `jail`, and
`networks`. `vm` and `jail` require an ID. The connector uses fixed HTTP GET
routes only; it cannot create, update, migrate, delete, access consoles, or
change permissions. The Apiary deployment must have API key authentication
enabled so its Viewer role is enforced.