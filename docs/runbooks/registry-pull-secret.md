# Runbook: registry pull-secret в Vault (secret/dps150/registry)

Статус: **засеяно и проверено вживую 2026-07-14** (пре-реквизит F-009).

## Зачем

Оба Deployment чарта `deploy/helm/dps150-web` тянут образы из приватного
GitLab registry `gitlab.zztopper.ru:5005` через `imagePullSecrets:
dps150-web-registry-creds`. Этот k8s Secret материализует VSO из Vault-пути
`secret/dps150/registry` (ключ `.dockerconfigjson`, тот же per-app паттерн,
что `secret/cattery/production/registry`).

**Без засеянного пути первый `deploy:prod` гарантированно красный**:
секрет не создастся → ImagePullBackOff → `helm --wait` timeout. В отличие от
DB-секрета (`secret/pg-cluster/dps150/database`, fail-soft, сеется I-002),
registry-секрет — жёсткий пре-реквизит деплоя.

## Что сделано (2026-07-14)

1. **Deploy token** проекта `applications/dps150-web` (id 41):
   name `dps150-k8s-pull`, username `k8s-deploy`, scope `read_registry`,
   без expiry (token id 10). Создание:

   ```bash
   glab api projects/41/deploy_tokens -X POST \
     -H "Content-Type: application/json" \
     --input - <<'EOF'
   {"name":"dps150-k8s-pull","username":"k8s-deploy","scopes":["read_registry"]}
   EOF
   # из ответа нужны .username и .token (token показывается ОДИН раз)
   ```

2. **Засев Vault** (admin-токен — см. `.env` в infrastructure/k8s-talos-cluster):

   ```bash
   export VAULT_ADDR=https://vault.r2bnj.ru
   REG=gitlab.zztopper.ru:5005 USER=k8s-deploy PASS=<deploy-token>
   jq -nc --arg reg "$REG" --arg u "$USER" --arg p "$PASS" \
     '{auths:{($reg):{username:$u,password:$p,auth:(($u+":"+$p)|@base64)}}}' \
     > /tmp/dockerconfig.json
   vault kv put secret/dps150/registry .dockerconfigjson=@/tmp/dockerconfig.json
   rm /tmp/dockerconfig.json
   ```

## Как проверялось (live)

- Deploy token аутентифицируется в registry: JWT-flow
  (`/jwt/auth?service=container_registry&scope=repository:applications/dps150-web/backend:pull`)
  выдаёт токен, `GET /v2/.../backend/tags/list` возвращает теги.
- Отрендеренный из чарта `VaultStaticSecret dps150-web-registry-creds`,
  применённый в ns `dps150`, синхронизировался `SYNCED/HEALTHY/READY=True`
  и создал Secret типа `kubernetes.io/dockerconfigjson`.
- Тестовый pod с `imagePullSecrets: dps150-web-registry-creds` успешно
  спуллил `backend:e34f3171` («Successfully pulled image») и завершился
  Completed. Тестовые VSS/pod после проверки удалены — kubectl-applied
  ресурсы без Helm ownership-меток сломали бы первый `helm upgrade --install`.

## Ротация

1. Создать новый deploy token (шаг 1), перезаписать `secret/dps150/registry`
   (шаг 2) — VSO подхватит в течение `refreshAfter: 300s`.
2. Revoke старого токена: `glab api projects/41/deploy_tokens/<id> -X DELETE`.
3. Уже запущенные pods не затронуты; pull нового образа проверит следующий
   деплой.
