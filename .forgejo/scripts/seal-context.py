#!/usr/bin/env python3
"""Materialize only reviewed tracked bytes before any registry credentials exist."""
import hashlib
import io
import json
import os
from pathlib import Path, PurePosixPath
import subprocess
import sys
import tarfile


def git(*args):
    return subprocess.check_output(["git", *args])


def inventory(ref):
    entries = {}
    for row in git("ls-tree", "-rz", ref).split(b"\0"):
        if not row:
            continue
        meta, raw = row.split(b"\t", 1)
        mode, kind, oid = meta.decode().split()
        name = raw.decode()
        if name.startswith(("backend/", "frontend/", "deploy/")) and not name.endswith("_test.go") and name != "frontend/playwright.config.ts":
            entries[name] = {"mode": mode, "object": oid}
    return entries


def main():
    target = Path(sys.argv[1]).resolve()
    revision = git("rev-parse", "HEAD").decode().strip()
    if os.environ.get("GITHUB_SHA", revision) != revision:
        raise SystemExit("checkout must match the exact workflow SHA")
    review = json.loads(git("show", "HEAD:.forgejo/reviewed-runtime.json"))
    if inventory("HEAD") != review["runtime"]:
        raise SystemExit("runtime content differs from the reviewed stage")
    target.mkdir(mode=0o700)  # A fresh directory; never merge runner contents.
    tracked = {}
    for row in git("ls-tree", "-rz", "HEAD").split(b"\0"):
        if row:
            meta, raw = row.split(b"\t", 1)
            mode, kind, oid = meta.decode().split()
            tracked[raw.decode()] = (mode, kind, oid)
    def blocked(path):
        return any(p in {".git", ".secrets", ".tokensave"} or p.startswith(".env") for p in PurePosixPath(path).parts)
    object_hash = git("rev-parse", "--show-object-format").decode().strip()
    records, excluded = [], []
    with tarfile.open(fileobj=io.BytesIO(git("archive", "--format=tar", "HEAD"))) as archive:
        for entry in archive:
            path = PurePosixPath(entry.name)
            if path.is_absolute() or ".." in path.parts:
                raise SystemExit("unsafe archive path")
            if blocked(str(path)):
                excluded.append(str(path))
                continue
            if entry.isdir():
                continue
            if not entry.isfile():
                raise SystemExit("only regular tracked files are permitted in a build context")
            data = archive.extractfile(entry).read()
            mode, kind, oid = tracked.get(str(path), (None, None, None))
            blob = hashlib.new(object_hash, b"blob " + str(len(data)).encode() + b"\0" + data).hexdigest()
            if kind != "blob" or mode not in {"100644", "100755"} or blob != oid or bool(entry.mode & 0o111) != (mode == "100755"):
                raise SystemExit("archive differs from exact tracked bytes")
            out = target.joinpath(*path.parts)
            out.parent.mkdir(parents=True, exist_ok=True)
            out.write_bytes(data)
            out.chmod(0o755 if entry.mode & 0o111 else 0o644)
            records.append({"path": str(path), "mode": oct(out.stat().st_mode & 0o777), "sha256": hashlib.sha256(data).hexdigest()})
    if {r["path"] for r in records} != {p for p in tracked if not blocked(p)}:
        raise SystemExit("archive omitted tracked paths (export-ignore is forbidden)")
    files = sorted(records, key=lambda r: r["path"])
    manifest = {"schema": 1, "integration_commit": revision,
                "source_tree": git("rev-parse", "HEAD^{tree}").decode().strip(),
                "phase": review["phase"], "reviewed_commit": review["reviewed_commit"],
                "native_base": review["native_base"], "native_template": review["native_template"],
                "files": files, "excluded": sorted(excluded)}
    manifest["context_sha256"] = hashlib.sha256(json.dumps(files, sort_keys=True, separators=(",", ":")).encode()).hexdigest()
    (target.parent / "source.json").write_text(json.dumps(manifest, indent=2) + "\n")
    print(manifest["context_sha256"])


if __name__ == "__main__":
    main()
