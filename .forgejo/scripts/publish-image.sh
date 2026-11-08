#!/usr/bin/env bash
# Application-local adapter of image.yml@daa2434f; only the native callers use it.
# Run seal-context.py in the preceding credential-free step first.
set -euo pipefail
set +x
: "${PUBLICATION_DIR:?}" "${IN_IMAGE:?}" "${IN_TAGS:?}" "${IN_DOCKERFILE:?}" "${BUILDKIT_HOST:?}" "${PKG_TOKEN:?}"
context="$PUBLICATION_DIR/context"
test -d "$context" && test -f "$PUBLICATION_DIR/source.json"
case "$IN_IMAGE:$IN_DOCKERFILE" in
  git.zztopper.ru/applications/dps150-web/backend:deploy/docker/Dockerfile.backend|git.zztopper.ru/applications/dps150-web/frontend:deploy/docker/Dockerfile.frontend) ;;
  *) echo 'unexpected native image input' >&2; exit 1 ;;
esac
export DOCKER_CONFIG="$PUBLICATION_DIR/auth"
# Auth is a sibling of the sealed source, never a BuildKit local input.
umask 077
mkdir "$DOCKER_CONFIG"
trap 'rm -rf "$DOCKER_CONFIG"; unset PKG_TOKEN' EXIT
export REGISTRY_PUSH_USER="${REGISTRY_PUSH_USER:-claude}"
python3 - <<'AUTH'
import base64, json, os
from pathlib import Path
value = base64.b64encode((os.environ["REGISTRY_PUSH_USER"] + ":" + os.environ["PKG_TOKEN"]).encode()).decode()
(Path(os.environ["DOCKER_CONFIG"]) / "config.json").write_text(json.dumps({"auths": {"git.zztopper.ru": {"auth": value}}}))
AUTH
unset PKG_TOKEN
names=$(printf '%s' "$IN_TAGS" | tr -d ' ' | tr ',' '\n' | sed '/^$/d' | sed "s#^#$IN_IMAGE:#" | paste -sd, -)
test -n "$names"
first="${names%%,*}"; rest="${names#"$first"}"; rest="${rest#,}"
# Preserve sha8/latest semantics; additionally bind the full integration SHA.
case ",$rest," in *",$IN_IMAGE:$GITHUB_SHA,"*) ;; *) rest="${rest:+$rest,}$IN_IMAGE:$GITHUB_SHA" ;; esac
read -r phase reviewed context_hash < <(python3 - "$PUBLICATION_DIR/source.json" <<'META'
import json, sys
m = json.load(open(sys.argv[1]))
print(m["phase"], m["reviewed_commit"], m["context_sha256"])
META
)
common=(--frontend dockerfile.v0 --local "context=$context" --local "dockerfile=$context/$(dirname "$IN_DOCKERFILE")"
  --opt "filename=$(basename "$IN_DOCKERFILE")" --opt platform=linux/amd64
  --opt "label:org.opencontainers.image.revision=$GITHUB_SHA"
  --opt "label:ru.r2bnj.dps.phase=$phase" --opt "label:ru.r2bnj.dps.reviewed=$reviewed"
  --opt "label:ru.r2bnj.dps.context-sha256=$context_hash")
buildctl --addr "$BUILDKIT_HOST" debug workers >/dev/null
# Native Forgejo workaround: push one tag, then tag its digest, then export cache.
buildctl --addr "$BUILDKIT_HOST" build "${common[@]}" \
  --import-cache "type=registry,ref=$IN_IMAGE:buildcache" \
  --metadata-file "$PUBLICATION_DIR/buildkit.json" --output "type=image,name=$first,push=true"
d=$(crane digest "$first")
[[ "$d" =~ ^sha256:[0-9a-f]{64}$ ]]
python3 - "$PUBLICATION_DIR/buildkit.json" "$d" <<'DIGEST'
import json, sys
if json.load(open(sys.argv[1])).get("containerimage.digest") != sys.argv[2]:
    raise SystemExit("registry digest differs from BuildKit output")
DIGEST
IFS=',' read -ra extra <<< "$rest"
for t in "${extra[@]}"; do [ -n "$t" ] || continue; crane tag "$IN_IMAGE@$d" "${t##*:}"; done
{
  echo "digest=$d"
  echo "context_sha256=$context_hash"
  echo "phase=$phase"
  echo "reviewed_commit=$reviewed"
} >> "$GITHUB_OUTPUT"
# Cache export remains warning-only, exactly as in the native v1 route.
buildctl --addr "$BUILDKIT_HOST" build "${common[@]}" \
  --export-cache "type=registry,ref=$IN_IMAGE:buildcache,mode=max" >/dev/null \
  && echo 'cache exported' || echo '::warning::cache export failed (non-critical)'
python3 - "$PUBLICATION_DIR/source.json" "$IN_IMAGE" "$d" "$IN_DOCKERFILE" <<'RECORD'
import hashlib, json, os, sys
from pathlib import Path
m = json.load(open(sys.argv[1]))
files = {f["path"]: f["sha256"] for f in m.pop("files")}
m.update(image=sys.argv[2], digest=sys.argv[3], platform="linux/amd64", dockerfile=sys.argv[4],
         dependency_sha256={p: files[p] for p in ["backend/go.mod", "backend/go.sum", "frontend/package.json", "frontend/package-lock.json"]},
         buildkit_metadata_sha256=hashlib.sha256((Path(sys.argv[1]).parent / "buildkit.json").read_bytes()).hexdigest(),
         verification="publication provenance only; actual image-content attestation required before rollout")
record = json.dumps(m, indent=2) + "\n"
(Path(sys.argv[1]).parent / "publication.json").write_text(record)
with open(os.environ.get("FORGEJO_STEP_SUMMARY", "/dev/null"), "a") as output:
    output.write("```json\n" + record + "```\n")
RECORD
