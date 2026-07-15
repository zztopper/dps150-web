# Runbook: registry pull-secret in Vault (secret/dps150/registry)

Status: **seeded and verified live 2026-07-14** (prerequisite for F-009).

## Why

Both Deployments in the `deploy/helm/dps150-web` chart pull images from the private
GitLab registry `git.example.com:5005` via `imagePullSecrets:
dps150-web-registry-creds`. This k8s Secret is materialized by VSO from the Vault path
`secret/dps150/registry` (key `.dockerconfigjson`, the same per-app pattern
as `secret/cattery/production/registry`).

**Without the path seeded, the first `deploy:prod` is guaranteed to fail**:
the secret is not created → ImagePullBackOff → `helm --wait` timeout. Unlike
the DB secret (`secret/pg-cluster/dps150/database`, fail-soft, seeded in I-002),
the registry secret is a hard deployment prerequisite.

## What was done (2026-07-14)

1. **Deploy token** for project `applications/dps150-web` (id 41):
   name `dps150-k8s-pull`, username `k8s-deploy`, scope `read_registry`,
   no expiry (token id 10). Creation:

   ```bash
   glab api projects/41/deploy_tokens -X POST \
     -H "Content-Type: application/json" \
     --input - <<'EOF'
   {"name":"dps150-k8s-pull","username":"k8s-deploy","scopes":["read_registry"]}
   EOF
   # from the response you need .username and .token (the token is shown ONCE)
   ```

2. **Seeding Vault** (admin token — see `.env` in infrastructure/k8s-talos-cluster):

   ```bash
   export VAULT_ADDR=https://vault.example.com
   REG=git.example.com:5005 USER=k8s-deploy PASS=<deploy-token>
   jq -nc --arg reg "$REG" --arg u "$USER" --arg p "$PASS" \
     '{auths:{($reg):{username:$u,password:$p,auth:(($u+":"+$p)|@base64)}}}' \
     > /tmp/dockerconfig.json
   vault kv put secret/dps150/registry .dockerconfigjson=@/tmp/dockerconfig.json
   rm /tmp/dockerconfig.json
   ```

## How it was verified (live)

- The deploy token authenticates to the registry: the JWT flow
  (`/jwt/auth?service=container_registry&scope=repository:applications/dps150-web/backend:pull`)
  issues a token, and `GET /v2/.../backend/tags/list` returns the tags.
- The `VaultStaticSecret dps150-web-registry-creds` rendered from the chart,
  applied in ns `dps150`, synced to `SYNCED/HEALTHY/READY=True`
  and created a Secret of type `kubernetes.io/dockerconfigjson`.
- A test pod with `imagePullSecrets: dps150-web-registry-creds` successfully
  pulled `backend:e34f3171` ("Successfully pulled image") and finished
  Completed. The test VSS/pod were deleted after verification — kubectl-applied
  resources without Helm ownership labels would have broken the first `helm upgrade --install`.

## Rotation

1. Create a new deploy token (step 1), overwrite `secret/dps150/registry`
   (step 2) — VSO picks it up within `refreshAfter: 300s`.
2. Revoke the old token: `glab api projects/41/deploy_tokens/<id> -X DELETE`.
3. Already-running pods are not affected; pulling the new image will be verified by the next
   deploy.
