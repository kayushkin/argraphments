#!/usr/bin/env bash
# Boot-and-answer smoke test for argraphments.
#
# Builds the server from THIS checkout, boots it against a throwaway SQLite
# database on a throwaway port, and drives a real create → read-back through the
# transcript API. The live service (:8086) and the live database — which holds
# personal conversation transcripts — are never touched.
#
# ------------------------------------------------------------------------
# NOTHING HERE MAY REACH AN LLM OR A PAID API.
#
# argraphments talks to Anthropic (claim extraction) and OpenAI (Whisper). The
# keys are supplied below as obvious dummies, and the routes that would spend
# them — /api/transcribe, /api/diarize, /api/analyze, /api/analyze-incremental,
# /api/import/youtube, /api/sample — are deliberately NOT driven. Everything
# asserted below is served out of SQLite.
#
# Do not "improve" this smoke by adding one of those routes: a boot smoke that
# costs money every night at 03:30 is a smoke people turn off.
# ------------------------------------------------------------------------
#
# What this actually proves, none of which `go build` can:
#
#   1. The binary boots at all. main() log.Fatal's on a missing API key, a
#      missing templates/ glob or an unopenable DB — three ways to die at start
#      that the compiler is blind to. The last step re-runs the binary with the
#      keys removed and requires it to fail, so we know that gate is real and
#      not just being skipped.
#   2. Route registration. main() registers its patterns TWICE, in a loop, under
#      both "" and "/argraphments" — including a bare "/" catch-all in each. Go
#      1.22+ ServeMux panics on a conflicting pattern at REGISTRATION time, so a
#      wrong prefix in that loop is a boot-time death that vets green. Both
#      prefixes are asserted below, because the deployed service is reached
#      through the /argraphments one and a smoke that only drove "" would not
#      notice if it disappeared.
#   3. SQLite actually works. The store opens with modernc.org/sqlite and runs
#      its migrations at boot; the create/read-back below is what proves the
#      schema, the slug generation and the transcript queries all still run.
#
# The SPA is NOT asserted on: static/dist is a build artifact and is not
# committed, so handleIndex correctly 404s from a clean clone. Guarding the
# frontend build is the TS/React tier's job, not this one.
#
# Exits 0 on success, non-zero on the first failing assertion. On failure the
# server log is dumped to stderr.
#
# Tunables:
#   E2E_PORT  — listen port (default 19125; NOT the live 8086)
#   E2E_KEEP  — set to "1" to leave $TMP_DIR around after the run

set -euo pipefail

REPO_DIR="$(cd "$(dirname "$0")/.." && pwd)"
PORT="${E2E_PORT:-19125}"
BASE="http://127.0.0.1:$PORT"

for bin in go curl jq; do
  if ! command -v "$bin" >/dev/null 2>&1; then
    echo "ERROR: required tool '$bin' not found on PATH" >&2
    exit 2
  fi
done

TMP_DIR="$(mktemp -d -t argraphments-e2e.XXXXXX)"
BIN_DIR="$TMP_DIR/bin"
RUN_DIR="$TMP_DIR/run"           # the server's CWD
DB_PATH="$TMP_DIR/data/argraphments.db"
LOG="$TMP_DIR/server.log"
mkdir -p "$BIN_DIR" "$RUN_DIR" "$TMP_DIR/data"

SERVER_PID=""
DUMPED=0

dump_log() {
  [ "$DUMPED" = "1" ] && return 0
  DUMPED=1
  if [ -s "$LOG" ]; then
    echo "----- server.log -----" >&2
    cat "$LOG" >&2
    echo "----------------------" >&2
  fi
}

cleanup() {
  # Capture the exit status FIRST — anything below would clobber it. Dumping the
  # log here (not only in fail()) is what covers the abort paths `set -e` takes
  # on its own, e.g. a `curl -fsS` returning non-2xx inside a $(...) assignment.
  local status=$?
  if [ -n "$SERVER_PID" ] && kill -0 "$SERVER_PID" 2>/dev/null; then
    kill "$SERVER_PID" 2>/dev/null || true
    wait "$SERVER_PID" 2>/dev/null || true
  fi
  [ "$status" -ne 0 ] && dump_log
  if [ "${E2E_KEEP:-}" = "1" ]; then
    echo "[e2e] keeping $TMP_DIR"
  else
    rm -rf "$TMP_DIR"
  fi
  return "$status"
}
trap cleanup EXIT INT TERM

step() { printf '\n==> %s\n' "$*"; }
fail() {
  echo "FAIL: $*" >&2
  dump_log
  exit 1
}

# Obvious non-credentials. main() only checks that they are NON-EMPTY; nothing
# below drives a route that would send them anywhere.
export ANTHROPIC_API_KEY="e2e-smoke-not-a-real-key"
export OPENAI_API_KEY="e2e-smoke-not-a-real-key"

# Refuse to run if something already owns the port — otherwise every assertion
# below would silently be testing THAT process (e.g. the live service).
if curl -fsS -o /dev/null --max-time 2 "$BASE/api/transcripts" 2>/dev/null; then
  echo "ERROR: something is already listening on $BASE — set E2E_PORT" >&2
  exit 2
fi

# Snapshot whether the CWD-relative artefacts already exist in the checkout, so
# the hermeticity check at the end can tell "this run leaked a path into the
# repo" from "the developer running this already had one".
#
# This matters because both are gitignored, so `git status` cannot see either,
# and because a real one is REAL DATA: a checkout that has ever run the server by
# hand holds ./argraphments.db and a ./uploads tree of personal audio. The guard
# clones from HEAD, so neither exists there and the check is exact; from a
# working checkout it stays exact instead of crying wolf. Nothing here deletes
# them — only notices what we created.
DB_PREEXISTED=0;      [ -e "$REPO_DIR/argraphments.db" ] && DB_PREEXISTED=1
UPLOADS_PREEXISTED=0; [ -e "$REPO_DIR/uploads" ]         && UPLOADS_PREEXISTED=1

step "build argraphments from $REPO_DIR"
cd "$REPO_DIR"
# Explicit -o: the main package is at the repo ROOT (there is a second main under
# cmd/migrate-speakers), so a bare `go build ./...` would drop binaries into the
# checkout.
CGO_ENABLED=0 go build -o "$BIN_DIR/argraphments" .
echo "    binary: $BIN_DIR/argraphments ($(ls -lh "$BIN_DIR/argraphments" | awk '{print $5}'))"

step "stage a run directory"
# main() reads templates/*.html and serves static/ relative to the CWD, and it
# mkdirs ./uploads. Copying those two trees into the temp dir — rather than
# running from the checkout — is what keeps the run from writing into the repo.
cp -r "$REPO_DIR/templates" "$RUN_DIR/templates"
cp -r "$REPO_DIR/static" "$RUN_DIR/static"
echo "    $RUN_DIR (templates + static)"

step "launch argraphments on :$PORT (db: $DB_PATH)"
cd "$RUN_DIR"
PORT="$PORT" ARGRAPHMENTS_DB="$DB_PATH" \
  "$BIN_DIR/argraphments" >"$LOG" 2>&1 &
SERVER_PID=$!
echo "    pid: $SERVER_PID"

# Poll for readiness — never sleep-and-hope. There is no /health route on this
# service, so readiness IS the first real API route. Bail the instant the process
# dies, so a boot-time log.Fatal or a mux panic surfaces as a clear failure
# rather than a fifteen-second mystery.
READY=0
for _ in $(seq 1 60); do
  if ! kill -0 "$SERVER_PID" 2>/dev/null; then
    fail "server exited during startup (see log below)"
  fi
  if curl -fsS -o /dev/null --max-time 2 "$BASE/api/transcripts" 2>/dev/null; then
    READY=1
    break
  fi
  sleep 0.25
done
[ "$READY" = "1" ] || fail "server did not answer on $BASE/api/transcripts within ~15s"

step "GET /api/transcripts — a fresh DB is empty"
LIST0="$(curl -fsS "$BASE/api/transcripts")"
echo "    $LIST0"
# An empty store serialises to null, not []; `null | length` is 0 either way.
# Anything else means we opened a POPULATED database — i.e. very possibly the
# live one, full of the user's real transcripts, and every assertion below would
# be measuring their data.
[ "$(jq -r 'length' <<<"$LIST0")" = "0" ] \
  || fail "expected an empty temp DB, got $(jq -r 'length' <<<"$LIST0") transcripts — is this the LIVE db?"

step "POST /api/session/new — create a transcript"
CREATED="$(curl -fsS -X POST "$BASE/api/session/new")"
echo "    $CREATED"
NEW_ID="$(jq -r '.id' <<<"$CREATED")"
NEW_SLUG="$(jq -r '.slug' <<<"$CREATED")"
[ -n "$NEW_ID" ] && [ "$NEW_ID" != "null" ]     || fail "/api/session/new returned no id: $CREATED"
[ -n "$NEW_SLUG" ] && [ "$NEW_SLUG" != "null" ] || fail "/api/session/new returned no slug: $CREATED"
# The slug is generated server-side, so a non-empty one proves the store wrote a
# row and read it back rather than the handler echoing a request it never got.
echo "    id=$NEW_ID slug=$NEW_SLUG"

step "GET /api/transcripts — read back what we wrote"
LIST1="$(curl -fsS "$BASE/api/transcripts")"
[ "$(jq -r --arg id "$NEW_ID" '[.[] | select((.id|tostring)==$id)] | length' <<<"$LIST1")" = "1" ] \
  || fail "transcript $NEW_ID did not appear in /api/transcripts: $LIST1"
echo "    it is in the list"

step "GET /api/transcripts/$NEW_SLUG — the slug lookup resolves"
# This route is served by the SAME handler as the list above, which distinguishes
# them by trimming the path prefix by hand. A change to that trimming breaks one
# and not the other, so both are asserted.
BY_SLUG="$(curl -fsS "$BASE/api/transcripts/$NEW_SLUG")"
# The detail route answers a composite — {transcript, speakers, speaker_info,
# messages, statements} — not the bare row the list route returns. Assert
# through .transcript, and assert the composite's other keys are present too:
# each is assembled by a separate store query, so a missing one means a query
# behind it broke without taking the request down with it.
GOT_SLUG="$(jq -r '.transcript.slug' <<<"$BY_SLUG")"
[ "$GOT_SLUG" = "$NEW_SLUG" ] \
  || fail "slug lookup returned the wrong transcript: got '$GOT_SLUG', want '$NEW_SLUG'"
[ "$(jq -r '.transcript.id' <<<"$BY_SLUG")" = "$NEW_ID" ] \
  || fail "slug lookup resolved to a different id than /api/session/new returned: $BY_SLUG"
[ "$(jq -r 'has("speakers") and has("statements") and has("messages")' <<<"$BY_SLUG")" = "true" ] \
  || fail "the transcript detail payload is missing its speakers/statements/messages keys: $BY_SLUG"
echo "    slug round-tripped, composite payload intact"

step "GET /argraphments/api/transcripts — the second prefix is registered too"
# The deployed service is reached under /argraphments. main() registers every
# route twice in a loop over both prefixes; if that loop ever stops covering the
# prefixed form, the live site 404s while a smoke driving only "" stays green.
PREFIXED="$(curl -fsS "$BASE/argraphments/api/transcripts")"
[ "$(jq -r --arg id "$NEW_ID" '[.[] | select((.id|tostring)==$id)] | length' <<<"$PREFIXED")" = "1" ] \
  || fail "/argraphments/api/transcripts did not return the transcript — the prefixed routes are not registered: $PREFIXED"
echo "    both prefixes serve the same data"

step "the API-key gate is real"
# main() log.Fatal's without ANTHROPIC_API_KEY / OPENAI_API_KEY. That gate is the
# reason this smoke has to supply dummies at all — so prove it still bites,
# otherwise the dummies above are cargo cult and a keyless deploy would boot into
# a service that 500s on its first real request instead of refusing to start.
set +e
( cd "$RUN_DIR" && env -u ANTHROPIC_API_KEY PORT="$PORT" ARGRAPHMENTS_DB="$DB_PATH" \
    "$BIN_DIR/argraphments" >"$TMP_DIR/nokey.log" 2>&1 )
NOKEY_RC=$?
set -e
[ "$NOKEY_RC" -ne 0 ] \
  || fail "the binary started WITHOUT ANTHROPIC_API_KEY — the startup key check is gone"
grep -q 'ANTHROPIC_API_KEY required' "$TMP_DIR/nokey.log" \
  || fail "keyless start failed, but not with the expected message: $(cat "$TMP_DIR/nokey.log")"
echo "    refuses to start without a key (rc=$NOKEY_RC)"

step "the run was hermetic"
[ -f "$DB_PATH" ] || fail "temp DB $DB_PATH was never created — which database did we just write to?"
# ARGRAPHMENTS_DB defaults to ./argraphments.db and main() mkdirs ./uploads, both
# CWD-relative — so a path that escaped the temp dir lands in the checkout. Only
# a path THIS RUN created is a failure (see the snapshot near the top).
if [ "$DB_PREEXISTED" = "0" ] && [ -e "$REPO_DIR/argraphments.db" ]; then
  fail "server wrote its DB INTO THE CHECKOUT at $REPO_DIR/argraphments.db — the ARGRAPHMENTS_DB override is not being honoured"
fi
if [ "$UPLOADS_PREEXISTED" = "0" ] && [ -e "$REPO_DIR/uploads" ]; then
  fail "server created ./uploads INTO THE CHECKOUT at $REPO_DIR/uploads — it is not running from the temp CWD"
fi
echo "    wrote only under $TMP_DIR"

step "SUCCESS — argraphments boots and answers"
echo "    server log: $LOG"
