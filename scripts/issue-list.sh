#!/usr/bin/env bash
# issue-list.sh — список Issues с возможностью фильтра по labels/state.
#
# Использование:
#   ./scripts/issue-list.sh                          # открытые
#   ./scripts/issue-list.sh closed                   # закрытые
#   ./scripts/issue-list.sh all                      # все
#   ./scripts/issue-list.sh open feature,backend     # с labels

set -euo pipefail

STATE="${1:-opened}"   # opened | closed | all
LABELS="${2:-}"

cd "$(git rev-parse --show-toplevel 2>/dev/null || pwd)"

if [ -f ".env" ]; then
    set -o allexport
    # shellcheck disable=SC1091
    source .env
    set +o allexport
fi

PROVIDER="${ISSUE_PROVIDER:-gitlab}"

case "$PROVIDER" in
    gitlab)
        REPO="${GITLAB_GROUP:?}/${GITLAB_PROJECT:?}"
        ARGS=(issue list --repo "$REPO")
        case "$STATE" in
            opened|open) ARGS+=(--opened) ;;
            closed)      ARGS+=(--closed) ;;
            all)         ARGS+=(--all) ;;
        esac
        if [ -n "$LABELS" ]; then
            IFS=',' read -ra LBL <<< "$LABELS"
            for l in "${LBL[@]}"; do
                ARGS+=(-l "$l")
            done
        fi
        glab "${ARGS[@]}"
        ;;
    github)
        REPO="${GITHUB_REPO:?}"
        case "$STATE" in
            opened|open) GH_STATE=open ;;
            closed)      GH_STATE=closed ;;
            all)         GH_STATE=all ;;
            *)           GH_STATE="$STATE" ;;
        esac
        ARGS=(issue list --repo "$REPO" --state "$GH_STATE" --limit 200)
        if [ -n "$LABELS" ]; then
            ARGS+=(--label "$LABELS")
        fi
        gh "${ARGS[@]}"
        ;;
    *)
        echo "ERROR: ISSUE_PROVIDER=$PROVIDER" >&2
        exit 1
        ;;
esac
