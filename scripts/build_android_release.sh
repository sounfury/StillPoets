#!/usr/bin/env bash
set -euo pipefail

# 使用固定 Android Release Keystore 本地构建 StillPoets APK（构建后自动恢复 mygo.json，不污染工作区）。
# 用法：
#   ANDROID_KEYSTORE_PATH=/home/ubuntu/.stillpoets-keys/stillpoets-release.jks \
#   ANDROID_KEYSTORE_PASSWORD=xxx \
#   ANDROID_KEY_ALIAS=stillpoets \
#   ./scripts/build_android_release.sh [arm64|universal]

ARCH="${1:-arm64}"
KS_PATH="${ANDROID_KEYSTORE_PATH:-${HOME}/.stillpoets-keys/stillpoets-release.jks}"
KEY_ALIAS="${ANDROID_KEY_ALIAS:-stillpoets}"

if [ ! -f "${KS_PATH}" ]; then
  echo "错误：未找到 keystore 文件：${KS_PATH}" >&2
  exit 1
fi
if [ -z "${ANDROID_KEYSTORE_PASSWORD:-}" ]; then
  echo "错误：请通过环境变量 ANDROID_KEYSTORE_PASSWORD 提供 keystore 密码" >&2
  exit 1
fi

BACKUP_JSON="$(mktemp)"
cp mygo.json "${BACKUP_JSON}"
cleanup() {
  cp "${BACKUP_JSON}" mygo.json
  rm -f "${BACKUP_JSON}"
}
trap cleanup EXIT

python3 - "${KS_PATH}" "${KEY_ALIAS}" <<'PY'
import json, sys
ks_path, alias = sys.argv[1], sys.argv[2]
with open("mygo.json", "r", encoding="utf-8") as f:
    cfg = json.load(f)
cfg.setdefault("android", {})["keystore"] = ks_path
cfg["android"]["keyAlias"] = alias
with open("mygo.json", "w", encoding="utf-8") as f:
    json.dump(cfg, f, indent=2, ensure_ascii=False)
    f.write("\n")
PY

export MYGO_ANDROID_KEYSTORE_PASSWORD="${ANDROID_KEYSTORE_PASSWORD}"
export MYGO_ANDROID_KEY_PASSWORD="${ANDROID_KEY_PASSWORD:-${ANDROID_KEYSTORE_PASSWORD}}"

MYGO_BIN="$(command -v mygo || echo "$(go env GOPATH)/bin/mygo")"
"${MYGO_BIN}" native check modules/widgetbridge
"${MYGO_BIN}" native check modules/appupdater

APP_VER=$(python3 -c 'import json; print(json.load(open("mygo.json"))["version"])')
if [ "${ARCH}" = "universal" ]; then
  "${MYGO_BIN}" build -platform android -o build
  SUFFIX="universal"
else
  "${MYGO_BIN}" build -platform "android/${ARCH}" -o build
  SUFFIX="${ARCH}"
fi

mkdir -p dist
APK_SRC=$(ls build/android/*.apk | head -n 1)
APK_NAME="StillPoets-${APP_VER}-android-${SUFFIX}.apk"
cp "${APK_SRC}" "dist/${APK_NAME}"
(cd dist && sha256sum "${APK_NAME}" > "${APK_NAME}.sha256" && sha256sum "${APK_NAME}" > SHA256SUMS.txt)
echo "构建完成：dist/${APK_NAME}"
cat dist/SHA256SUMS.txt
