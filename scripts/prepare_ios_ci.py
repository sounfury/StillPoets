#!/usr/bin/env python3
"""Prepare mygo.json and GitHub Actions environment variables for iOS builds.

Supports:
- Enterprise wildcard .mobileprovision (e.g. TEAM.* or TEAM.com.example.*)
- Ad-Hoc / Development / App Store profiles (single wildcard or dual main+widget profiles)
- Simulator builds without signing credentials
"""

from __future__ import annotations

import argparse
import datetime
import json
import os
import plistlib
import subprocess
import sys
from pathlib import Path


def parse_bool(raw: str) -> bool:
    return raw.strip().lower() in ("1", "true", "yes", "y", "on")


def decode_profile(profile_path: Path, raw_plist_path: Path | None = None) -> dict:
    if raw_plist_path is not None:
        return plistlib.loads(raw_plist_path.read_bytes())
    proc = subprocess.run(
        ["security", "cms", "-D", "-i", str(profile_path)],
        check=False,
        capture_output=True,
    )
    if proc.returncode != 0:
        err = proc.stderr.decode("utf-8", errors="replace").strip()
        raise RuntimeError(f"无法解码描述文件 {profile_path}: {err}")
    return plistlib.loads(proc.stdout)


def detect_distribution(plist: dict) -> str:
    ent = plist.get("Entitlements") or {}
    if plist.get("ProvisionsAllDevices") is True:
        return "enterprise"
    if ent.get("get-task-allow") is True:
        return "development"
    if plist.get("ProvisionedDevices"):
        return "ad-hoc"
    return "app-store"


def pattern_matches(pattern: str, bundle_id: str) -> bool:
    if pattern == bundle_id:
        return True
    if pattern.endswith("*"):
        prefix = pattern[:-1]
        return bundle_id.startswith(prefix)
    return False


def resolve_app_bundle_id(pattern: str, desired: str) -> str:
    if pattern_matches(pattern, desired):
        return desired
    if pattern.endswith(".*"):
        base = pattern[:-2]
        return f"{base}.stillpoets"
    if pattern.endswith("*"):
        base = pattern[:-1].rstrip(".")
        return f"{base}.stillpoets" if base else desired
    return pattern


def extract_app_id(plist: dict) -> tuple[str, str]:
    ent = plist.get("Entitlements") or {}
    app_id = str(ent.get("application-identifier") or "")
    if "." not in app_id:
        raise RuntimeError(f"描述文件缺少合法的 application-identifier: {app_id!r}")
    prefix, pattern = app_id.split(".", 1)
    return prefix, pattern


def check_expiration(plist: dict, label: str) -> None:
    exp = plist.get("ExpirationDate")
    if isinstance(exp, datetime.datetime):
        now = datetime.datetime.now(datetime.timezone.utc)
        exp_utc = exp if exp.tzinfo is not None else exp.replace(tzinfo=datetime.timezone.utc)
        if exp_utc < now:
            raise RuntimeError(f"{label} 已于 {exp_utc.strftime('%Y-%m-%d')} 过期，请更换有效的描述文件")


def write_env(env_path: Path | None, values: dict[str, str]) -> None:
    lines = [f"{k}={v}" for k, v in values.items()]
    if env_path is not None:
        with env_path.open("a", encoding="utf-8") as fh:
            for line in lines:
                fh.write(line + "\n")
    for line in lines:
        print(f"[ios-ci] {line}")


def main() -> int:
    ap = argparse.ArgumentParser(description="Prepare StillPoets iOS CI configuration")
    ap.add_argument("--config", default="mygo.json")
    ap.add_argument("--mode", choices=["unsigned", "signed", "simulator"], required=True)
    ap.add_argument("--include-widget", default="true")
    ap.add_argument("--profile", default="")
    ap.add_argument("--widget-profile", default="")
    ap.add_argument("--profile-plist", default="", help="Testing only: pre-decoded main profile plist")
    ap.add_argument("--widget-profile-plist", default="", help="Testing only: pre-decoded widget profile plist")
    ap.add_argument("--bundle-id-override", default="")
    ap.add_argument("--appex-path", default="native/ios/build/StillPoetsWidget.appex")
    ap.add_argument("--github-env", default="")
    args = ap.parse_args()

    cfg_path = Path(args.config)
    cfg = json.loads(cfg_path.read_text(encoding="utf-8"))
    ios_cfg = cfg.setdefault("ios", {})
    info_plist = ios_cfg.setdefault("infoPlist", {})

    include_widget = parse_bool(args.include_widget)
    version = str(cfg.get("version") or "0.1.0")
    build_number = str(ios_cfg.get("buildNumber") or version)
    desired_bundle_id = (args.bundle_id_override or cfg.get("identifier") or "app.stillpoets.client").strip()

    env_out: dict[str, str] = {
        "APP_VERSION": version,
        "APP_BUILD_NUMBER": build_number,
        "STILLPOETS_APP_GROUP": "",
        "STILLPOETS_KEYCHAIN_GROUP": "",
    }

    if args.mode in ("unsigned", "simulator"):
        app_bundle_id = desired_bundle_id
        widget_bundle_id = f"{app_bundle_id}.widget"
        cfg["identifier"] = app_bundle_id
        info_plist.pop("StillPoetsKeychainGroup", None)
        info_plist.pop("StillPoetsAppGroup", None)
        if include_widget:
            ios_cfg["extensions"] = [{"path": args.appex_path}]
        else:
            ios_cfg.pop("extensions", None)
        cfg_path.write_text(json.dumps(cfg, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")

        env_out["APP_BUNDLE_ID"] = app_bundle_id
        env_out["WIDGET_BUNDLE_ID"] = widget_bundle_id
        env_out["BUILD_WIDGET"] = "true" if include_widget else "false"
        env_out["IOS_DISTRIBUTION"] = args.mode
        write_env(Path(args.github_env) if args.github_env else None, env_out)
        return 0

    if not args.profile and not args.profile_plist:
        raise RuntimeError("signed 模式必须提供 --profile (.mobileprovision)")

    profile_path = Path(args.profile).resolve() if args.profile else Path("main.mobileprovision").resolve()
    raw_plist = Path(args.profile_plist) if args.profile_plist else None
    main_plist = decode_profile(profile_path, raw_plist)
    check_expiration(main_plist, "主 App 描述文件")

    distribution = detect_distribution(main_plist)
    team_ids = main_plist.get("TeamIdentifier") or []
    team_id = str(team_ids[0]) if team_ids else ""
    app_id_prefix, app_id_pattern = extract_app_id(main_plist)

    app_bundle_id = resolve_app_bundle_id(app_id_pattern, desired_bundle_id)
    widget_bundle_id = f"{app_bundle_id}.widget"
    if app_bundle_id != desired_bundle_id:
        print(
            f"::notice::描述文件允许的 Bundle ID 规则为 {app_id_pattern}，已自动调整主 App Bundle ID 为 {app_bundle_id}"
        )

    main_ent = main_plist.get("Entitlements") or {}
    main_app_groups = list(main_ent.get("com.apple.security.application-groups") or [])
    main_kc_groups = list(main_ent.get("keychain-access-groups") or [])

    build_widget = include_widget
    widget_profile_path = profile_path
    widget_app_groups = main_app_groups

    if include_widget:
        if args.widget_profile or args.widget_profile_plist:
            widget_profile_path = (
                Path(args.widget_profile).resolve()
                if args.widget_profile
                else Path("widget.mobileprovision").resolve()
            )
            w_raw = Path(args.widget_profile_plist) if args.widget_profile_plist else None
            w_plist = decode_profile(widget_profile_path, w_raw)
            check_expiration(w_plist, "小组件描述文件")
            _, w_pattern = extract_app_id(w_plist)
            if not pattern_matches(w_pattern, widget_bundle_id):
                raise RuntimeError(
                    f"小组件描述文件规则 {w_pattern} 与目标 Bundle ID {widget_bundle_id} 不匹配"
                )
            w_ent = w_plist.get("Entitlements") or {}
            widget_app_groups = list(w_ent.get("com.apple.security.application-groups") or [])
        elif not pattern_matches(app_id_pattern, widget_bundle_id):
            print(
                f"::warning::当前描述文件为精确 Bundle ID（{app_id_pattern}）而非通配符，且未提供小组件独立描述文件（IOS_WIDGET_PROVISIONING_PROFILE_BASE64），本次将仅打包主 App（不含桌面小组件）。"
            )
            build_widget = False

    common_app_groups = [g for g in main_app_groups if g in widget_app_groups]
    app_group = str(common_app_groups[0]) if common_app_groups else ""

    shared_kc_group = f"{app_id_prefix}.stillpoets.shared"
    kc_allowed = any(
        g in (f"{app_id_prefix}.*", "*", shared_kc_group)
        for g in main_kc_groups
    )
    keychain_group = shared_kc_group if kc_allowed else ""

    cfg["identifier"] = app_bundle_id
    if team_id:
        ios_cfg["team"] = team_id
    ios_cfg["distribution"] = distribution
    ios_cfg["provisioningProfile"] = str(profile_path)
    info_plist["StillPoetsKeychainGroup"] = keychain_group
    if app_group:
        info_plist["StillPoetsAppGroup"] = app_group
    else:
        info_plist.pop("StillPoetsAppGroup", None)

    if build_widget:
        ios_cfg["extensions"] = [
            {
                "path": args.appex_path,
                "provisioningProfile": str(widget_profile_path),
            }
        ]
    else:
        ios_cfg.pop("extensions", None)

    cfg_path.write_text(json.dumps(cfg, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")

    env_out.update(
        {
            "APP_BUNDLE_ID": app_bundle_id,
            "WIDGET_BUNDLE_ID": widget_bundle_id,
            "BUILD_WIDGET": "true" if build_widget else "false",
            "IOS_DISTRIBUTION": distribution,
            "IOS_TEAM_ID": team_id,
            "STILLPOETS_APP_GROUP": app_group,
            "STILLPOETS_KEYCHAIN_GROUP": keychain_group,
        }
    )
    write_env(Path(args.github_env) if args.github_env else None, env_out)
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except Exception as exc:
        print(f"::error::{exc}", file=sys.stderr)
        raise SystemExit(1)
