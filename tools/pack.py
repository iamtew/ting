#!/usr/bin/env python3
"""Stage working tree, zip it, or scp the artifact to prod. Cross-platform."""

from __future__ import annotations

import argparse
import fnmatch
import os
import shutil
import subprocess
import sys
import zipfile
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent

EXCLUDE_DIR_NAMES = {
    ".git",
    "dist",
    "__pycache__",
    ".venv",
    "venv",
    "node_modules",
    ".cursor",
}
EXCLUDE_FILE_NAMES = {".env", ".DS_Store", "Thumbs.db"}
EXCLUDE_GLOBS = {"*.pyc", "*.pyo", "*.zip"}


def _excluded(rel: Path) -> bool:
    parts = rel.parts
    if any(p in EXCLUDE_DIR_NAMES for p in parts):
        return True
    name = rel.name
    if name in EXCLUDE_FILE_NAMES:
        return True
    return any(fnmatch.fnmatch(name, g) for g in EXCLUDE_GLOBS)


def cmd_stage(out: Path) -> None:
    if out.exists():
        shutil.rmtree(out)
    out.mkdir(parents=True)

    for dirpath, dirnames, filenames in os.walk(ROOT):
        current = Path(dirpath)
        rel_dir = current.relative_to(ROOT)
        # prune excluded dirs in-place
        dirnames[:] = [
            d
            for d in dirnames
            if d not in EXCLUDE_DIR_NAMES and not _excluded(rel_dir / d)
        ]
        for name in filenames:
            src = current / name
            rel = src.relative_to(ROOT)
            if _excluded(rel):
                continue
            dest = out / rel
            dest.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(src, dest)

    print(f"staged {ROOT} -> {out}")


def cmd_zip(src: Path, out: Path) -> None:
    if not src.is_dir():
        sys.exit(f"missing stage dir: {src} (run build first)")
    out.parent.mkdir(parents=True, exist_ok=True)
    if out.exists():
        out.unlink()
    with zipfile.ZipFile(out, "w", compression=zipfile.ZIP_DEFLATED) as zf:
        for path in sorted(src.rglob("*")):
            if path.is_file():
                zf.write(path, path.relative_to(src).as_posix())
    print(f"wrote {out} ({out.stat().st_size} bytes)")


def cmd_clean(dist: Path) -> None:
    if dist.exists():
        shutil.rmtree(dist)
    dist.mkdir(parents=True)
    (dist / ".gitkeep").touch()
    print(f"cleaned {dist}")


def cmd_ship(artifact: Path, host: str, path: str, user: str) -> None:
    if not host:
        sys.exit("TNG_PROD_HOST is required (or pass --host)")
    if not path:
        sys.exit("TNG_PROD_PATH is required (or pass --path)")
    if not artifact.is_file():
        sys.exit(f"missing artifact: {artifact} (run package first)")
    target_host = f"{user}@{host}" if user else host
    dest = f"{target_host}:{path.rstrip('/')}/"
    cmd = ["scp", str(artifact), dest]
    print(" ".join(cmd))
    subprocess.check_call(cmd)


def main() -> None:
    p = argparse.ArgumentParser(description=__doc__)
    sub = p.add_subparsers(dest="cmd", required=True)

    s = sub.add_parser("stage", help="copy working tree into a stage directory")
    s.add_argument("--out", type=Path, required=True)

    z = sub.add_parser("zip", help="zip a stage directory")
    z.add_argument("--src", type=Path, required=True)
    z.add_argument("--out", type=Path, required=True)

    h = sub.add_parser("ship", help="scp artifact to prod")
    h.add_argument("--artifact", type=Path, required=True)
    h.add_argument("--host", default=os.environ.get("TNG_PROD_HOST", ""))
    h.add_argument("--path", default=os.environ.get("TNG_PROD_PATH", ""))
    h.add_argument("--user", default=os.environ.get("TNG_PROD_USER", ""))

    c = sub.add_parser("clean", help="remove dist artifacts")
    c.add_argument("--dist", type=Path, required=True)

    args = p.parse_args()
    if args.cmd == "stage":
        cmd_stage(args.out.resolve())
    elif args.cmd == "zip":
        cmd_zip(args.src.resolve(), args.out.resolve())
    elif args.cmd == "ship":
        cmd_ship(args.artifact.resolve(), args.host, args.path, args.user)
    elif args.cmd == "clean":
        cmd_clean(args.dist.resolve())


if __name__ == "__main__":
    main()
