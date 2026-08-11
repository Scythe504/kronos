#!/usr/bin/env bash
set -e

# ==========================================
# 1. Top-Level Variable Initialization
# ==========================================
VERSION="${KRONOS_VERSION:-{{ .Version }}}"
if [ "$VERSION" = "{{ .Version }}" ] || [ -z "$VERSION" ]; then
  VERSION="v0.1.0"
fi

REPO="${KRONOS_REPO:-scythe504/kronos}"

DEFAULT_MASTER_URL="{{ .MasterURL }}"
if [ "$DEFAULT_MASTER_URL" = "{{ .MasterURL }}" ] || [ -z "$DEFAULT_MASTER_URL" ]; then
  DEFAULT_MASTER_URL="http://localhost:8080"
fi

MASTER_URL="${KRONOS_MASTER_URL:-$DEFAULT_MASTER_URL}"
ALLOWED_SLUGS="${KRONOS_ALLOWED_SLUGS:-}"
TASK_UNIT="${KRONOS_TASK_UNIT:-cpu}"

# Directories & File Paths
if [ "$(id -u)" -eq 0 ]; then
  CONFIG_DIR="/etc/kronos"
  BIN_DIR="/usr/local/bin"
else
  CONFIG_DIR="$HOME/.kronos"
  BIN_DIR="$HOME/.local/bin"
fi

CONF_FILE="$CONFIG_DIR/agent.conf"
SERVICE_FILE="/etc/systemd/system/kronos.service"

# UI / Terminal Formatting
BOLD="\033[1m"
GREEN="\033[32m"
CYAN="\033[36m"
YELLOW="\033[33m"
RED="\033[31m"
RESET="\033[0m"

echo -e "${BOLD}${CYAN}=== Kronos Node Agent Setup (${VERSION}) ===${RESET}\n"

# Interactive prompt for Master URL if running interactively and not specified in environment
if [ -z "$KRONOS_MASTER_URL" ] && [ -t 0 ]; then
  read -r -p "Enter Master Server URL [default: $DEFAULT_MASTER_URL]: " INPUT_URL
  MASTER_URL="${INPUT_URL:-$DEFAULT_MASTER_URL}"
fi

echo -e "${GREEN}Configured Master URL: $MASTER_URL${RESET}"
echo -e "${GREEN}Configured Task Unit: $TASK_UNIT${RESET}\n"

# ==========================================
# 2. Detect Operating System & Architecture
# ==========================================
OS_RAW="$(uname -s)"
case "$OS_RAW" in
  Linux*)   OS="Linux" ;;
  Darwin*)  OS="Darwin" ;;
  MINGW*|MSYS*|CYGWIN*) OS="Windows" ;;
  *) echo -e "${RED}Unsupported OS: $OS_RAW${RESET}"; exit 1 ;;
esac

ARCH_RAW="$(uname -m)"
case "$ARCH_RAW" in
  x86_64|amd64)   ARCH="x86_64" ;;
  arm64|aarch64) ARCH="arm64" ;;
  i386|i686|386) ARCH="i386" ;;
  *) echo -e "${RED}Unsupported Architecture: $ARCH_RAW${RESET}"; exit 1 ;;
esac

if [ "$OS" = "Windows" ]; then
  EXT="zip"
else
  EXT="tar.gz"
fi

ARCHIVE_NAME="kronos_${OS}_${ARCH}.${EXT}"
DOWNLOAD_URL="https://github.com/${REPO}/releases/download/${VERSION}/${ARCHIVE_NAME}"

VERSION_NO_V="${VERSION#v}"
CHECKSUMS_NAME="kronos_${VERSION_NO_V}_checksums.txt"
CHECKSUMS_URL="https://github.com/${REPO}/releases/download/${VERSION}/${CHECKSUMS_NAME}"

# ==========================================
# 3. Create Directories & Download Assets
# ==========================================
mkdir -p "$CONFIG_DIR" "$BIN_DIR"

TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT

echo -e "${CYAN}Downloading Kronos Node Agent (${ARCHIVE_NAME})...${RESET}"
echo -e "URL: ${DOWNLOAD_URL}"

if command -v curl >/dev/null 2>&1; then
  curl -fsSL "$DOWNLOAD_URL" -o "$TMP_DIR/$ARCHIVE_NAME"
  curl -fsSL "$CHECKSUMS_URL" -o "$TMP_DIR/$CHECKSUMS_NAME" 2>/dev/null || true
elif command -v wget >/dev/null 2>&1; then
  wget -qO "$TMP_DIR/$ARCHIVE_NAME" "$DOWNLOAD_URL"
  wget -qO "$TMP_DIR/$CHECKSUMS_NAME" "$CHECKSUMS_URL" 2>/dev/null || true
else
  echo -e "${RED}Error: Neither curl nor wget is available.${RESET}"
  exit 1
fi

# ==========================================
# 4. Verify Checksum & Extract Binary
# ==========================================
if [ -f "$TMP_DIR/$CHECKSUMS_NAME" ]; then
  echo -e "${CYAN}Verifying SHA256 checksum...${RESET}"
  if command -v sha256sum >/dev/null 2>&1; then
    (cd "$TMP_DIR" && grep "$ARCHIVE_NAME" "$CHECKSUMS_NAME" | sha256sum -c -) || echo -e "${YELLOW}Warning: Checksum verification warning.${RESET}"
  elif command -v shasum >/dev/null 2>&1; then
    (cd "$TMP_DIR" && grep "$ARCHIVE_NAME" "$CHECKSUMS_NAME" | shasum -a 256 -c -) || echo -e "${YELLOW}Warning: Checksum verification warning.${RESET}"
  fi
fi

echo -e "${CYAN}Extracting archive...${RESET}"
if [ "$EXT" = "zip" ]; then
  unzip -o "$TMP_DIR/$ARCHIVE_NAME" -d "$TMP_DIR"
else
  tar -xzf "$TMP_DIR/$ARCHIVE_NAME" -C "$TMP_DIR"
fi

BIN_NAME="kronos"
if [ "$OS" = "Windows" ]; then
  BIN_NAME="kronos.exe"
fi

if [ ! -f "$TMP_DIR/$BIN_NAME" ]; then
  echo -e "${RED}Error: Executable $BIN_NAME was not found in extracted archive.${RESET}"
  exit 1
fi

mv "$TMP_DIR/$BIN_NAME" "$BIN_DIR/$BIN_NAME"
chmod +x "$BIN_DIR/$BIN_NAME"
echo -e "${GREEN}Binary installed to $BIN_DIR/$BIN_NAME${RESET}"

# ==========================================
# 5. Save Configuration to agent.conf
# ==========================================
cat << EOF > "$CONF_FILE"
# Kronos Node Agent Configuration
MASTER_URL=$MASTER_URL
ALLOWED_SLUGS=$ALLOWED_SLUGS
TASK_UNIT=$TASK_UNIT
EOF

echo -e "${GREEN}Configuration saved to $CONF_FILE${RESET}"

# ==========================================
# 6. Systemd Installation (Linux Root)
# ==========================================
if [ "$OS" = "Linux" ] && [ "$(id -u)" -eq 0 ] && command -v systemctl >/dev/null 2>&1; then
  cat << EOF > "$SERVICE_FILE"
[Unit]
Description=Kronos Worker Node Agent Daemon
After=network.target

[Service]
Type=simple
EnvironmentFile=$CONF_FILE
ExecStart=$BIN_DIR/kronos
Restart=always
RestartSec=5s

[Install]
WantedBy=multi-user.target
EOF

  systemctl daemon-reload
  echo -e "${GREEN}Systemd service installed at $SERVICE_FILE${RESET}"
  echo -e "To start the agent: ${BOLD}systemctl enable --now kronos${RESET}"
fi

echo -e "\n${BOLD}${GREEN}=== Node Setup Completed Successfully! ===${RESET}"

