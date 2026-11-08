# Native publication integration

This source retains the current Forgejo CI, its backend/frontend/browser gates,
master-only two-image publication, sha8/latest tags, linux/amd64 platform, registry,
client-side CI_PACKAGES_TOKEN authority and warning-only separate cache export.
The app-local adapter follows image.yml at
`daa2434f659a4c7aaf00d9848c526896ed0b9648`. It replaces the two native callers;
it does not add a publication workflow. A full integration SHA tag is additional.
Do not use `latest` or sha8 as an immutable release identity.

Before registry auth exists, `seal-context.py` validates the reviewed runtime Git
blob inventory and exact workflow SHA. It archives HEAD and admits regular tracked
files only, omitting `.env*`, `.tokensave`, `.secrets` and `.git` at every depth.
The context manifest binds path, executable mode and SHA256; its hash is attached
to image labels and the publication summary. Auth remains in a sibling temporary
directory. A trap and the always cleanup remove it. The output digest must match
both BuildKit metadata and the registry. Both image jobs must agree on the phase,
reviewed commit and sealed context before the aggregate binding summary succeeds.
This is publication provenance, not an actual image-content attestation.

The E2E gate compiles `internal/api/isolated_e2e_test.go`, not `cmd/server`. That
fixture calls the existing NewRouter with an in-process emulator and fresh local
SQLite. History, journal and automation workers support the complete existing
browser suite. No production config loader, physical transport factory, MQTT or
notification worker is called. Notifications report configured=false. This gate
does not test production bootstrap or the sequence/charge/IV run managers.
The wrapper clears ambient environment, uses a fresh HOME, and downloads locked
dependencies/browser before creating a new user/network namespace. Only loopback
is brought up. Missing unshare/ip/user-namespace support fails the gate; there is
no fallback to the host network. Playwright never reuses listeners. The fixture
binds only 127.0.0.1:18080 and creates no metrics9090 listener.

No image has been built/published and no E2E service started by preparing this
source. Offline fixture tests, syntax checks, selected API/config unit tests and
compilation are the authorized local validation. Full CI requires a later
execution scope. Do not skip E2E to publish if runner isolation fails.

Before rollout, record immutable base/platform manifest and config digests,
BuildKit/frontend and compiler identities, dependency lock hashes, APK package
inputs/inventory and CI run evidence. Current Dockerfiles intentionally retain
the exact reviewed bytes and moving base/APK inputs; do not claim reproducible
builds from source metadata alone. Pull the returned digest in an authorized
artifact-only gate; verify OCI blob hashes and extract safely without executing
an entrypoint. Compare backend ELF/build metadata and binary SHA256 against the
same pinned build inputs. Compare every frontend dist file and nginx template
against an independently produced expected manifest; reject extra files/links.

Release order remains:

1. Publish and attest both images for integrated phase A; bind their shared tag
   and digests on the old chart, wait for the one Recreate backend to be Ready.
2. Verify metrics9090; apply the reviewed narrow chart with A's exact image
   binding. Do not use its stale cedcb264 placeholder. Preserve unrelated chart
   changes, replica count, egress and registry-pull-secret/VSO paths.
3. Only then publish/attest both integrated phase B images and change the image
   binding, retaining narrow9090 policies. No collector deployment is required.

Rollback after narrowing selects the retained verified A images with the narrow
chart and finite authenticated WS sessions. Never select the pre-A runtime under
the narrow chart. Check private pull authority for both Forgejo images without
printing tokens or Secret.data; a VSO synced condition alone is insufficient.
