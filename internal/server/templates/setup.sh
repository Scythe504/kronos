#!/usr/bin/env bash
set -e

BOLD="\033[1m"
GREEN="\033[32m"
CYAN="\033[36m"
YELLOW="\033[33m"
RESET="\033[0m"

echo -e "${BOLD}${CYAN}=== Kronos Node Agent Setup ===${RESET}\n"

# 1. Resolve Master URL (Environment variable or prompt)
DEFAULT_MASTER_URL="{{ .MasterURL }}"
if [ -n "$KRONOS_MASTER_URL" ]; then
  MASTER_URL="$KRONOS_MASTER_URL"
else
  read -p "Enter Master Server URL [default: $DEFAULT_MASTER_URL]: " MASTER_URL
  MASTER_URL=${MASTER_URL:-$DEFAULT_MASTER_URL}
fi

# 3. Resolve Allowed Slugs (Default empty -> Master auto-assigns)
ALLOWED_SLUGS="${KRONOS_ALLOWED_SLUGS:-}"

# 4. Resolve Task Unit (Default cpu -> CPU only)
TASK_UNIT="${KRONOS_TASK_UNIT:-cpu}"

echo -e "${GREEN}Configured Master URL: $MASTER_URL${RESET}"
echo -e "${GREEN}Configured Task Unit: $TASK_UNIT${RESET}\n"

# 5. Determine Config Path & Create Directory
if [ "$(id -u)" -eq 0 ]; then
  CONFIG_DIR="/etc/kronos"
  BIN_DIR="/usr/local/bin"
else
  CONFIG_DIR="$HOME/.kronos"
  BIN_DIR="$HOME/.local/bin"
fi

mkdir -p "$CONFIG_DIR" "$BIN_DIR"

# 6. Write agent.conf
CONF_FILE="$CONFIG_DIR/agent.conf"
cat << EOF > "$CONF_FILE"
# Kronos Node Agent Configuration
MASTER_URL=$MASTER_URL
ALLOWED_SLUGS=$ALLOWED_SLUGS
TASK_UNIT=$TASK_UNIT
EOF

echo -e "${GREEN}Configuration saved to $CONF_FILE${RESET}"

# 7. Systemd Installation (Linux Root)
if [ "$(id -u)" -eq 0 ] && command -v systemctl >/dev/null 2>&1; then
  SERVICE_FILE="/etc/systemd/system/kronos-agent.service"
  cat << EOF > "$SERVICE_FILE"
[Unit]
Description=Kronos Worker Node Agent Daemon
After=network.target

[Service]
Type=simple
EnvironmentFile=$CONF_FILE
ExecStart=$BIN_DIR/kronos-orchestrator
Restart=always
RestartSec=5s

[Install]
WantedBy=multi-user.target
EOF

  systemctl daemon-reload
  echo -e "${GREEN}Systemd service installed at $SERVICE_FILE${RESET}"
  echo -e "To start the agent: ${BOLD}systemctl enable --now kronos-agent${RESET}"
fi

echo -e "\n${BOLD}${GREEN}=== Node Setup Completed Successfully! ===${RESET}"
