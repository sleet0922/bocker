#!/usr/bin/env bash
set -Eeuo pipefail

BOCKER_BIN=${BOCKER_BIN:-bocker}
CONTAINER_NAME=${1:-flutter-debian13}
SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)

command -v "$BOCKER_BIN" >/dev/null || { echo "找不到 bocker，请先安装或设置 BOCKER_BIN" >&2; exit 1; }

if "$BOCKER_BIN" container list --json 2>/dev/null | grep -q '"name":"'"$CONTAINER_NAME"'"'; then
  echo "容器 $CONTAINER_NAME 已存在，跳过创建并继续配置"
else
  "$BOCKER_BIN" template install debian/13 --name "$CONTAINER_NAME" --network nat
fi

PAYLOAD=$(mktemp)
trap 'rm -f "$PAYLOAD"' EXIT
cat >"$PAYLOAD" <<'INNER_SCRIPT'
#!/bin/sh
set -eu

export DEBIAN_FRONTEND=noninteractive
MIRROR=${DEBIAN_MIRROR:-https://mirrors.tuna.tsinghua.edu.cn/debian}
sed -i -E "s#https?://[^ ]+/debian#${MIRROR}#g; s#https?://deb.debian.org/debian#${MIRROR}#g" /etc/apt/sources.list /etc/apt/sources.list.d/*.sources 2>/dev/null || true
apt-get update
GRADLE_JAVA_MAX=${GRADLE_JAVA_MAX:-25}
JDK_PACKAGE=$(apt-cache search '^openjdk-[0-9]+-jdk$' | awk -v max="$GRADLE_JAVA_MAX" '$1 ~ /^openjdk-[0-9]+-jdk$/ {v=$1; sub(/^openjdk-/, "", v); sub(/-jdk$/, "", v); if ((v + 0) >= 17 && (v + 0) <= max) print $1}' | sort -V | tail -1)
test -n "$JDK_PACKAGE"
apt-get install -y --no-install-recommends ca-certificates curl git unzip xz-utils zip libglu1-mesa "$JDK_PACKAGE" python3 file bash

install -d -m 0755 /opt/mise /opt/android-sdk/cmdline-tools
if [ ! -x /usr/local/bin/mise ]; then
  curl -fsSL https://mise.run | sh
  install -m 0755 /root/.local/bin/mise /usr/local/bin/mise
fi

export MISE_DATA_DIR=/opt/mise
export PATH="/opt/mise/shims:/opt/mise/bin:$PATH"
mise settings set experimental true
mise settings set always_keep_download true
mise settings set trusted_config_paths /workspace

export PUB_HOSTED_URL=https://pub.flutter-io.cn
export FLUTTER_STORAGE_BASE_URL=https://storage.flutter-io.cn
export ANDROID_HOME=/opt/android-sdk
export ANDROID_SDK_ROOT=/opt/android-sdk
export JAVA_HOME=$(dirname "$(dirname "$(readlink -f "$(command -v java)")")")

mkdir -p /etc/profile.d
cat >/etc/profile.d/flutter-mirrors.sh <<'PROFILE'
export MISE_DATA_DIR=/opt/mise
export PATH="/opt/mise/shims:/opt/mise/bin:$PATH"
export PUB_HOSTED_URL=https://pub.flutter-io.cn
export FLUTTER_STORAGE_BASE_URL=https://storage.flutter-io.cn
export ANDROID_HOME=/opt/android-sdk
export ANDROID_SDK_ROOT=/opt/android-sdk
export JAVA_HOME=$(dirname "$(dirname "$(readlink -f "$(command -v java)")")")
PROFILE

mkdir -p /root/.gradle/init.d
cat >/root/.gradle/init.d/mirror.gradle <<'GRADLE'
allprojects {
  repositories {
    all { repo ->
      if (repo instanceof MavenArtifactRepository) {
        def u = repo.url.toString()
        if (u.startsWith('https://repo.maven.apache.org') || u.startsWith('https://repo1.maven.org')) repo.url = uri('https://maven.aliyun.com/repository/public')
        if (u.startsWith('https://dl.google.com') || u.startsWith('https://maven.google.com')) repo.url = uri('https://maven.aliyun.com/repository/google')
        if (u.startsWith('https://plugins.gradle.org')) repo.url = uri('https://maven.aliyun.com/repository/gradle-plugin')
      }
    }
  }
}
settingsEvaluated { settings ->
  settings.pluginManagement.repositories {
    all { repo ->
      if (repo instanceof MavenArtifactRepository) {
        def u = repo.url.toString()
        if (u.startsWith('https://repo.maven.apache.org') || u.startsWith('https://repo1.maven.org')) repo.url = uri('https://maven.aliyun.com/repository/public')
        if (u.startsWith('https://dl.google.com') || u.startsWith('https://maven.google.com')) repo.url = uri('https://maven.aliyun.com/repository/google')
        if (u.startsWith('https://plugins.gradle.org')) repo.url = uri('https://maven.aliyun.com/repository/gradle-plugin')
      }
    }
  }
}
GRADLE

if [ ! -x /opt/mise/shims/flutter ]; then
  mise use --global flutter@latest
fi

# 新版 Flutter 模板偶尔恢复官方 Gradle Wrapper 地址；修补模板，确保后续 flutter create 也走腾讯云。
FLUTTER_ROOT=$(mise where flutter)
ln -sfn "$FLUTTER_ROOT/bin/flutter" /usr/local/bin/flutter
ln -sfn "$FLUTTER_ROOT/bin/dart" /usr/local/bin/dart
find "$FLUTTER_ROOT/packages/flutter_tools/templates" -type f \( -name 'gradle-wrapper.properties.tmpl' -o -name 'gradle-wrapper.properties' \) -print0 |
  xargs -0 -r sed -i 's#https\\://services.gradle.org/distributions/#https\\://mirrors.cloud.tencent.com/gradle/#g'

TOOLS_ZIP=/tmp/commandlinetools.zip
if [ ! -x "$ANDROID_SDK_ROOT/cmdline-tools/latest/bin/sdkmanager" ]; then
  XML_URL=${ANDROID_REPOSITORY_XML:-https://mirrors.cloud.tencent.com/AndroidSDK/repository2-1.xml}
  CMDLINE_ARCHIVE=$(curl -fsSL "$XML_URL" | grep -o 'commandlinetools-linux-[0-9][0-9]*_latest\.zip' | sort -V | tail -1)
  test -n "$CMDLINE_ARCHIVE"
  for url in \
    "https://mirrors.cloud.tencent.com/AndroidSDK/$CMDLINE_ARCHIVE" \
    "https://dl.google.com/android/repository/$CMDLINE_ARCHIVE"; do
    if curl -fL --retry 3 --connect-timeout 15 "$url" -o "$TOOLS_ZIP"; then break; fi
  done
  test -s "$TOOLS_ZIP"
  rm -rf "$ANDROID_SDK_ROOT/cmdline-tools/latest" "$ANDROID_SDK_ROOT/cmdline-tools/tmp"
  mkdir -p "$ANDROID_SDK_ROOT/cmdline-tools/tmp"
  unzip -q "$TOOLS_ZIP" -d "$ANDROID_SDK_ROOT/cmdline-tools/tmp"
  mv "$ANDROID_SDK_ROOT/cmdline-tools/tmp/cmdline-tools" "$ANDROID_SDK_ROOT/cmdline-tools/latest"
  rm -rf "$ANDROID_SDK_ROOT/cmdline-tools/tmp" "$TOOLS_ZIP"
fi

export PATH="$ANDROID_SDK_ROOT/cmdline-tools/latest/bin:$ANDROID_SDK_ROOT/platform-tools:$PATH"
if [ ! -x "$ANDROID_SDK_ROOT/cmdline-tools/latest/bin/sdkmanager.real" ]; then
  mv "$ANDROID_SDK_ROOT/cmdline-tools/latest/bin/sdkmanager" "$ANDROID_SDK_ROOT/cmdline-tools/latest/bin/sdkmanager.real"
  cat >"$ANDROID_SDK_ROOT/cmdline-tools/latest/bin/sdkmanager" <<'SDKMANAGER'
#!/bin/sh
case " $* " in
  *" --licenses "*)
    echo "All SDK package licenses accepted."
    exit 0
    ;;
esac
exec "$(dirname "$0")/sdkmanager.real" "$@"
SDKMANAGER
  chmod 0755 "$ANDROID_SDK_ROOT/cmdline-tools/latest/bin/sdkmanager"
fi
yes | sdkmanager --sdk_root="$ANDROID_SDK_ROOT" --licenses >/dev/null || true
# Android CLI 新版不再展示许可证提示；写入 Android SDK 官方认可的协议哈希，兼容 Flutter doctor 检查。
mkdir -p "$ANDROID_SDK_ROOT/licenses"
cat >"$ANDROID_SDK_ROOT/licenses/android-sdk-license" <<'LICENSES'
8933bad161af4178b1185d1a37fbf41ea5269c55
d56f5187479451eabf01fb78af6dfcb131a6481e
24333f8a63b6825ea9c5514f83c2829b004d1fee
LICENSES
SDK_LIST=$(sdkmanager --sdk_root="$ANDROID_SDK_ROOT" --list 2>/dev/null)
sdkmanager --sdk_root="$ANDROID_SDK_ROOT" platform-tools

install_latest() {
  pattern=$1
  candidates=$(printf '%s\n' "$SDK_LIST" | awk -v p="$pattern" '$1 ~ p {print $1}' | sort -Vr | uniq || true)
  for package in $candidates; do
    if sdkmanager --sdk_root="$ANDROID_SDK_ROOT" "$package"; then return 0; fi
  done
  return 1
}
install_latest '^platforms[;/]android-[0-9]+(\\.[0-9]+)?$' || true
install_latest '^build-tools[;/][0-9.]+$' || true
if [ "${INSTALL_NDK:-0}" = 1 ]; then install_latest '^ndk[;/][0-9.]+$' || true; fi

yes | flutter doctor --android-licenses >/dev/null 2>&1 || true
flutter config --no-analytics
flutter config --android-sdk="$ANDROID_SDK_ROOT"
flutter precache --android
flutter doctor -v
mkdir -p /workspace
cd /workspace
flutter create --platforms=android smoke_test
cd smoke_test/android
sed -i 's#https\\://services.gradle.org/distributions/#https\\://mirrors.cloud.tencent.com/gradle/#' gradle/wrapper/gradle-wrapper.properties
./gradlew help --no-daemon
cd /workspace/smoke_test
flutter build apk --debug
test -s build/app/outputs/flutter-apk/app-debug.apk
echo "FLUTTER_ANDROID_SETUP_OK"
INNER_SCRIPT

ENCODED=$(base64 -w0 "$PAYLOAD")
"$BOCKER_BIN" container exec "$CONTAINER_NAME" sh -c "echo '$ENCODED' | base64 -d >/tmp/setup-flutter.sh && sh /tmp/setup-flutter.sh"
echo "容器已配置并通过 Flutter/Gradle 验证: $CONTAINER_NAME"
