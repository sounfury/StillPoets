#!/usr/bin/env python3
"""Build a CI-only mygo CLI binary with MYGO_IOS_UNSIGNED=1 support.

When MYGO_IOS_UNSIGNED=1 is set during `mygo build -platform ios`, this patches
a temporary copy of cmd/mygo/ios.go so MyGo still compiles the real iphoneos/arm64
binary, Swift native modules, and embedded .appex extensions, then ad-hoc signs
and packs the unsigned .ipa (for local/on-device signing tools like 全能签/轻松签).
"""

from __future__ import annotations

import argparse
import os
import shutil
import stat
import subprocess
import sys
from pathlib import Path


def make_writable(root: Path) -> None:
    for dirpath, dirnames, filenames in os.walk(root):
        p = Path(dirpath)
        p.chmod(p.stat().st_mode | stat.S_IWUSR)
        for name in filenames:
            fp = p / name
            if not fp.is_symlink():
                fp.chmod(fp.stat().st_mode | stat.S_IWUSR)


def patch_ios_go(ios_go: Path) -> None:
    src = ios_go.read_text(encoding="utf-8")
    old_block_1 = "\tvar prof *profile\n\tif !simulator {"
    new_block_1 = '\tunsigned := os.Getenv("MYGO_IOS_UNSIGNED") == "1"\n\tvar prof *profile\n\tif !simulator && !unsigned {'
    if old_block_1 not in src:
        raise RuntimeError("未能在 cmd/mygo/ios.go 中匹配到 pickProfile 代码块，请检查 mygo 版本变化")
    src = src.replace(old_block_1, new_block_1, 1)

    old_block_2 = "\t\tif simulator {\n\t\t\terr = signForSimulator(bundle)\n\t\t} else {\n\t\t\terr = prof.sign(opts.work, bundle, c, exts)\n\t\t}"
    new_block_2 = "\t\tif simulator || unsigned {\n\t\t\terr = signForSimulator(bundle)\n\t\t} else {\n\t\t\terr = prof.sign(opts.work, bundle, c, exts)\n\t\t}"
    if old_block_2 not in src:
        raise RuntimeError("未能在 cmd/mygo/ios.go 中匹配到 signing 代码块，请检查 mygo 版本变化")
    src = src.replace(old_block_2, new_block_2, 1)

    ios_go.write_text(src, encoding="utf-8")


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--work-dir", required=True)
    ap.add_argument("--out-bin", required=True)
    ap.add_argument("--dry-run", action="store_true", help="Only verify patch against local module cache without compiling")
    args = ap.parse_args()

    mod_dir = subprocess.check_output(
        ["go", "list", "-m", "-f", "{{.Dir}}", "github.com/egoist/mygo"],
        text=True,
    ).strip()
    if not mod_dir or not Path(mod_dir).is_dir():
        raise RuntimeError(f"无法定位 github.com/egoist/mygo 模块目录: {mod_dir!r}")

    src_copy = Path(args.work_dir) / "mygo-src"
    if src_copy.exists():
        make_writable(src_copy)
        shutil.rmtree(src_copy)
    shutil.copytree(mod_dir, src_copy, symlinks=True)
    make_writable(src_copy)

    patch_ios_go(src_copy / "cmd" / "mygo" / "ios.go")
    print(f"[mygo-ci] Patched {src_copy / 'cmd' / 'mygo' / 'ios.go'} for MYGO_IOS_UNSIGNED=1")

    if args.dry_run:
        return 0

    out_bin = Path(args.out_bin).resolve()
    out_bin.parent.mkdir(parents=True, exist_ok=True)
    subprocess.run(
        ["go", "build", "-o", str(out_bin), "./cmd/mygo"],
        cwd=str(src_copy),
        check=True,
    )
    print(f"[mygo-ci] Built CI mygo CLI at {out_bin}")
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except Exception as exc:
        print(f"::error::{exc}", file=sys.stderr)
        raise SystemExit(1)
