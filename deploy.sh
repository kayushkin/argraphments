#!/usr/bin/env bash
set -euo pipefail

# Builds argraphments, installs the binary AND the systemd unit, restarts the
# service, and refuses to report success unless the thing actually answers.
#
# It replaces update-argraphments.sh, which rsync'd this tree over ssh to a
# remote "kayushkin.com" and rebuilt it there under ~/argraphments. That remote
# is this box, and ~/argraphments no longer exists -- but the live unit still
# pointed at it as WorkingDirectory/EnvironmentFile, so systemd could not chdir
# or read the env and failed every start with an opaque `result: resources`.
# Restart=always + RestartSec=3 turned that into a silent crash loop that ran up
# 838,924 restarts before anyone looked. The in-repo unit template had been
# corrected months earlier; nothing ever copied it onto the live unit.
#
# Hence the rule this script enforces: the unit template in this repo is the
# single source of truth, and deploying installs it. The live unit cannot drift
# from the template without the next deploy pulling it back.

REPO_DIR="$(cd "$(dirname "$0")" && pwd)"
UNIT_NAME="argraphments.service"
REPO_UNIT="$REPO_DIR/$UNIT_NAME"
LIVE_UNIT="/etc/systemd/system/$UNIT_NAME"

cd "$REPO_DIR"

# mise's shim re-reads its config from CWD and will not exec go from an
# untrusted path; resolve the real binary the way the build guard does.
export PATH="$HOME/.local/share/mise/shims:$PATH"

[ -f "$REPO_UNIT" ] || { echo "ERROR: no unit template at $REPO_UNIT"; exit 1; }

# Everything the deploy needs is already declared in the unit. Read it from
# there rather than restating it here -- a second copy is a second thing to
# drift. (A sibling repo's deploy.sh hardcoded a BIN_NAME it had inherited from
# the repo it was cloned from, and would have overwritten another live service's
# binary.)
unit_field() { grep -E "^$1=" "$REPO_UNIT" | head -1 | cut -d= -f2-; }
BINARY_PATH="$(unit_field ExecStart)"
BINARY_NAME="$(basename "$BINARY_PATH")"
WORK_DIR="$(unit_field WorkingDirectory)"
ENV_FILE="$(grep -E '^EnvironmentFile=' "$REPO_UNIT" | head -1 | cut -d= -f2-)"
PORT="$(grep -E '^Environment=PORT=' "$REPO_UNIT" | head -1 | cut -d= -f3)"

echo "==> Unit declares:"
echo "    ExecStart         = $BINARY_PATH"
echo "    WorkingDirectory  = $WORK_DIR"
echo "    EnvironmentFile   = $ENV_FILE"
echo "    PORT              = $PORT"

# Preflight the two paths systemd will need. Both were wrong on the live unit for
# months, and systemd reports either as `result: resources` with no clue which --
# so check them here, where we can say which one is missing, before we restart
# anything. This is the check that would have caught the crash loop on day one.
echo "==> Preflight..."
[ -d "$WORK_DIR" ]  || { echo "ERROR: WorkingDirectory $WORK_DIR does not exist -- systemd cannot chdir and will crash-loop"; exit 1; }
[ -f "$ENV_FILE" ]  || { echo "ERROR: EnvironmentFile $ENV_FILE does not exist -- systemd cannot load it and will crash-loop"; exit 1; }
echo "    WorkingDirectory and EnvironmentFile both exist"

echo "==> Building $BINARY_NAME..."
go build -o "$BINARY_NAME" .
echo "    built: $(ls -lh "$BINARY_NAME" | awk '{print $5}')"

# Gate on vet, not `go test ./...`. TestYouTubeImportAPI calls the live YouTube
# API and fails with "rate limited" on a perfectly good tree; a deploy gate that
# goes red for reasons unrelated to the deploy is one people learn to ignore. vet
# still typechecks the test files, so a test that stops compiling is caught here.
echo "==> Vetting..."
go vet ./...
echo "    vet clean"

echo "==> Installing unit -> $LIVE_UNIT..."
if sudo cmp -s "$REPO_UNIT" "$LIVE_UNIT" 2>/dev/null; then
  echo "    live unit already matches the template"
else
  echo "    live unit differs from the template (or is absent) -- installing:"
  sudo diff "$LIVE_UNIT" "$REPO_UNIT" 2>/dev/null | sed 's/^/      /' || true
  sudo cp "$REPO_UNIT" "$LIVE_UNIT"
fi
sudo systemctl daemon-reload

# Stage next to the target and rename into place. A plain `cp` over the live
# binary fails with ETXTBSY ("text file busy") because the running service still
# has it open for execution; rename(2) is atomic, swaps the inode, and leaves the
# running process on the old one until we restart it below. Same filesystem, so
# the mv cannot degrade into a copy.
echo "==> Installing binary -> $BINARY_PATH..."
sudo cp "$BINARY_NAME" "$BINARY_PATH.new"
sudo chmod 755 "$BINARY_PATH.new"
sudo mv "$BINARY_PATH.new" "$BINARY_PATH"

echo "==> Restarting $UNIT_NAME..."
sudo systemctl reset-failed "$UNIT_NAME" 2>/dev/null || true
sudo systemctl restart "$UNIT_NAME"

# Poll for readiness; do not sleep and hope. A fixed sleep either races a slow
# start (deploy.sh exits 1 on a good binary it has already installed, which is
# indistinguishable from reporting no deploys at all) or pads every good deploy.
echo "==> Waiting for :$PORT to answer..."
READY_TIMEOUT=45
for i in $(seq 1 "$READY_TIMEOUT"); do
  if curl -fsS --max-time 5 "http://localhost:$PORT/api/transcripts" >/dev/null 2>&1; then
    echo "    ready after ${i}s"
    break
  fi
  if ! systemctl is-active --quiet "$UNIT_NAME"; then
    echo "ERROR: $UNIT_NAME died while starting up"
    sudo journalctl -u "$UNIT_NAME" -n 30 --no-pager
    exit 1
  fi
  if [ "$i" -eq "$READY_TIMEOUT" ]; then
    echo "ERROR: $UNIT_NAME still not answering :$PORT after ${READY_TIMEOUT}s"
    sudo journalctl -u "$UNIT_NAME" -n 30 --no-pager
    exit 1
  fi
  sleep 1
done

# Smoke a route that reads the DB, and assert on the body. "The port is open" only
# proves a process bound it; this proves the deployed binary can reach its store
# through the WorkingDirectory the unit just handed it -- the thing that was broken.
echo "==> Smoke test..."
BODY="$(curl -fsS --max-time 10 "http://localhost:$PORT/api/transcripts")"
case "$BODY" in
  '['*) echo "    /api/transcripts returned a JSON array" ;;
  *)    echo "ERROR: /api/transcripts did not return a JSON array; got: ${BODY:0:200}"; exit 1 ;;
esac

# is-active is the last word: a unit that is up now but scheduled to restart is
# not a successful deploy.
systemctl is-active --quiet "$UNIT_NAME" || { echo "ERROR: $UNIT_NAME is not active"; exit 1; }
RESTARTS="$(systemctl show "$UNIT_NAME" -p NRestarts --value)"
echo "    $UNIT_NAME active, NRestarts=$RESTARTS"
echo "    smoke test OK"

# The vhost is what puts argraphments.com on the internet: TLS, the HTTP->HTTPS
# redirect, the ACME challenge root and the 50M upload limit the audio uploads
# need. It is part of the deploy, not something to hand-edit in /etc/nginx — a
# vhost no repo tracks survives only as long as this box does.
echo "==> Installing nginx vhost..."
./deploy/nginx/install.sh

echo "==> Done."
