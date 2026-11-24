#!/usr/bin/env python3
"""Generate or update StillPoets apps.json for QNQ (全能签), ESign (轻松签), AltStore, and SideStore."""

from __future__ import annotations

import argparse
import datetime
import json
import re
from pathlib import Path

REPO_OWNER = "sounfury"
REPO_NAME = "StillPoets"
RAW_BASE = f"https://raw.githubusercontent.com/{REPO_OWNER}/{REPO_NAME}/main"
REPO_URL = f"https://github.com/{REPO_OWNER}/{REPO_NAME}"

APP_DESCRIPTION = (
    "让你记住，自己依然是诗人（Still a Poet）。\n\n"
    "StillPoets 是一款私有化（BYOD）现代诗集阅读、独立散页管理与桌面小组件联动应用：\n"
    "• 双入口结构：EPUB 诗集解析导入与独立散页摘录互不污染\n"
    "• 沉浸阅读体验：左右跟手滑切诗、自定义字体与四档字重、偶遇进度保护\n"
    "• 三套出版主题：画册风 / 刊物风 / 暗夜极简（支持跟随系统深色模式）\n"
    "• 原生桌面小组件：iOS WidgetKit 小/中/大号卡片 + 原地换诗 + 点击高亮直达原诗\n"
    "• 私有 WebDAV 同步：兼容坚果云、Nextcloud、群晖 NAS 与 AList/115，凭据仅存系统 Keychain"
)

SCREENSHOT_URLS = [
    f"{RAW_BASE}/docs/prototypes/stillpoets-v3-shelf-fragments.png",
    f"{RAW_BASE}/docs/prototypes/stillpoets-v3-editorial.png",
    f"{RAW_BASE}/docs/prototypes/stillpoets-v3-settings.png",
]


def clean_release_notes(raw: str) -> str:
    """Extract readable plain-text changelog from Markdown release notes."""
    text = raw.strip()
    if not text:
        return "常规体验优化与问题修复。"
    if "### 本次核心更新" in text:
        text = text.split("### 本次核心更新", 1)[1].strip()
    lines: list[str] = []
    for line in text.splitlines():
        s = line.strip()
        if not s:
            continue
        if s.startswith("### "):
            break
        s = re.sub(r"\*\*(.+?)\*\*", r"\1", s)
        s = s.replace("`", "")
        if s.startswith("- "):
            s = "• " + s[2:]
        lines.append(s)
    return "\n".join(lines) if lines else text


def build_source(
    existing: dict | None,
    version: str,
    tag: str,
    ipa_name: str,
    size: int,
    date_iso: str,
    description: str,
    min_os: str = "17.0",
) -> dict:
    clean_ver = version.lstrip("vV").strip()
    download_url = f"{REPO_URL}/releases/download/{tag}/{ipa_name}"
    icon_url = f"{RAW_BASE}/resources/icon.png"
    source_url = f"{RAW_BASE}/apps.json"

    prev_versions: list[dict] = []
    if existing and isinstance(existing.get("apps"), list) and existing["apps"]:
        raw_versions = existing["apps"][0].get("versions")
        if isinstance(raw_versions, list):
            prev_versions = [
                v for v in raw_versions
                if isinstance(v, dict) and str(v.get("version", "")).lstrip("vV") != clean_ver
            ]

    latest_entry = {
        "version": clean_ver,
        "date": date_iso,
        "localizedDescription": description,
        "downloadURL": download_url,
        "size": int(size),
        "minOSVersion": min_os,
    }
    versions = [latest_entry, *prev_versions]

    app_entry = {
        "name": "StillPoets",
        "bundleIdentifier": "app.stillpoets.client",
        "developerName": REPO_OWNER,
        "subtitle": "私有诗集阅读、独立散页与桌面诗笺小组件",
        "localizedDescription": APP_DESCRIPTION,
        "iconURL": icon_url,
        "tintColor": "BC5B46",
        "category": "books",
        "type": "0",
        "lock": "0",
        "isLanZouCloud": "0",
        "version": clean_ver,
        "versionDate": date_iso,
        "versionDescription": description,
        "downloadURL": download_url,
        "size": int(size),
        "screenshotURLs": SCREENSHOT_URLS,
        "versions": versions,
    }

    return {
        "name": "StillPoets 官方源",
        "subtitle": "让你记住，自己依然是诗人（Still a Poet）",
        "description": "StillPoets 私有化（BYOD）多端现代诗集阅读、独立散页与桌面小组件官方软件源。",
        "message": "StillPoets 官方 iOS 自签软件源\n免费开源，仅分发未签名原包，请使用全能签 / 轻松签 / AltStore / SideStore 自签安装。",
        "identifier": "app.stillpoets.source",
        "sourceURL": source_url,
        "sourceicon": icon_url,
        "iconURL": icon_url,
        "website": REPO_URL,
        "tintColor": "BC5B46",
        "apps": [app_entry],
    }


def main() -> int:
    ap = argparse.ArgumentParser(description="Update apps.json iOS sideload source")
    ap.add_argument("--output", default="apps.json")
    ap.add_argument("--version", required=True)
    ap.add_argument("--tag", required=True)
    ap.add_argument("--ipa-name", required=True)
    ap.add_argument("--size", type=int, required=True)
    ap.add_argument("--date", default="")
    ap.add_argument("--notes", default="")
    ap.add_argument("--notes-file", default="")
    ap.add_argument("--min-os", default="17.0")
    args = ap.parse_args()

    out_path = Path(args.output)
    existing = None
    if out_path.exists():
        try:
            existing = json.loads(out_path.read_text(encoding="utf-8"))
        except (OSError, json.JSONDecodeError):
            existing = None

    date_iso = args.date.strip() or datetime.datetime.now(datetime.UTC).strftime("%Y-%m-%dT%H:%M:%SZ")
    raw_notes = args.notes
    if args.notes_file:
        raw_notes = Path(args.notes_file).read_text(encoding="utf-8")
    desc = clean_release_notes(raw_notes)

    payload = build_source(
        existing=existing,
        version=args.version,
        tag=args.tag,
        ipa_name=args.ipa_name,
        size=args.size,
        date_iso=date_iso,
        description=desc,
        min_os=args.min_os,
    )
    out_path.write_text(json.dumps(payload, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    print(f"Updated {out_path} -> v{args.version.lstrip('vV')} ({args.ipa_name}, {args.size} bytes)")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
