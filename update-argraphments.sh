#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REMOTE="kayushkincom@kayushkin.com"

if [ -f "$HOME/bin/.env" ]; then
  set -a
  source "$HOME/bin/.env"
  set +a
fi

echo "Syncing files to server..."
sshpass -p "$KAYUSHKINCOM_PASS" rsync -avz --delete --exclude='.git' --exclude='uploads/' "$SCRIPT_DIR/" "$REMOTE:~/argraphments/"

echo "Building and restarting on server..."
sshpass -p "$KAYUSHKINCOM_PASS" ssh "$REMOTE" 'export PATH="$HOME/.local/share/mise/shims:$HOME/.local/bin:$PATH" && cd ~/argraphments && go build -o argraphments-server && printf "%s\n" "'"$KAYUSHKINCOM_PASS"'" | sudo -S mv argraphments-server /usr/local/bin/ && printf "%s\n" "'"$KAYUSHKINCOM_PASS"'" | sudo -S systemctl restart argraphments'

echo "Done."
