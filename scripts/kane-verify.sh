#!/bin/sh
# kane-verify.sh - run the checks in kane/ against a running Fixpoint console
# and, as a Claude Code Stop hook, hand any failure back to the agent.
#
# Stop-hook contract (verified against two independent write-ups of the Claude
# Code hooks reference; docs.claude.com itself was not reachable from the
# machine this was written on, so treat the exact field set as near-certain
# rather than certain):
#   exit 0 with no stdout, or with "{}"                 -> the agent may stop
#   exit 0 with {"decision":"block","reason":"<text>"}  -> the agent keeps going
#                                                          and reads <text>
#   The hook input on stdin carries "stop_hook_active": true once the agent has
#   already been blocked once, which is the documented guard against looping.
#
# It also runs by hand: scripts/kane-verify.sh (no stdin) prints the results and
# exits non-zero when a check failed.
#
# Environment:
#   FIXPOINT_URL   console origin to check (default http://127.0.0.1:7777)
#   FIXPOINT_AGENT set to 1 by Fixpoint around its own agent calls; the hook
#                  then does nothing at all, so Fixpoint's agent cannot drive
#                  Fixpoint's own verification
#   KANE_BIN       Kane CLI binary (default kane-cli)
#   KANE_TIMEOUT   per-check timeout in seconds (default 240)
#   KANE_CHECKS    directory of check files (default <repo>/kane)

set -u

MAX_BLOCKS=3
URL=${FIXPOINT_URL:-http://127.0.0.1:7777}
KANE=${KANE_BIN:-kane-cli}
TIMEOUT=${KANE_TIMEOUT:-240}

root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
checks=${KANE_CHECKS:-$root/kane}

# hook is 1 when we were given a hook payload on stdin, 0 when a person ran us.
hook=0
payload=
if [ ! -t 0 ]; then
  payload=$(cat)
  case $payload in
    *hook_event_name*) hook=1 ;;
  esac
fi

allow() {
  [ "$hook" -eq 1 ] && printf '{}\n'
  exit 0
}

note() {
  printf '%s\n' "$1" >&2
}

# Fixpoint's own agent must never trigger this: it would make the agent that is
# patching the target app wait on checks of the console that drives it.
if [ "${FIXPOINT_AGENT:-}" = "1" ]; then
  note "kane-verify: FIXPOINT_AGENT=1, so Fixpoint's own agent is running; skipping."
  allow
fi

# Already blocked once in this stop: the documented loop guard.
case $payload in
  *'"stop_hook_active":true'* | *'"stop_hook_active": true'*)
    note "kane-verify: stop_hook_active is true; letting the agent stop."
    allow
    ;;
esac

if [ ! -d "$checks" ]; then
  note "kane-verify: no check directory at $checks; skipping."
  allow
fi

if ! curl -fs -m 5 -o /dev/null "$URL/api/system"; then
  note "kane-verify: nothing is answering at $URL; start Fixpoint (go run ./cmd/fixpoint up) first. Skipping."
  allow
fi

if ! command -v "$KANE" >/dev/null 2>&1; then
  note "kane-verify: $KANE is not installed, so nothing can be verified; skipping."
  note "kane-verify: install it with: npm install -g @testmuai/kane-cli && kane-cli login"
  allow
fi

# field <file> <prefix> reads a one-line header field, e.g. "Start URL: ".
field() {
  sed -n "s|^$2||p" "$1" | sed -n 1p
}

# objective <file> is the file's last paragraph: the Kane objective itself.
objective() {
  awk 'BEGIN{RS=""} {last=$0} END{print last}' "$1" | tr '\n' ' ' |
    sed -e 's/  */ /g' -e 's/^ //' -e 's/ $//'
}

# Session-scoped counter, so a run of checks that keeps failing cannot hold the
# agent for ever.
session=$(printf '%s' "$payload" | sed -n 's/.*"session_id" *: *"\([^"]*\)".*/\1/p')
[ -n "$session" ] || session=manual
counter=/tmp/fixpoint-kane-verify-$(printf '%s' "$session" | tr -c 'A-Za-z0-9_-' '_').count

work=${TMPDIR:-/tmp}/fixpoint-kane-verify
mkdir -p "$work" || {
  note "kane-verify: could not create $work; skipping."
  allow
}

failures=
failed=0
total=0

for file in "$checks"/*.md; do
  [ -f "$file" ] || continue
  total=$((total + 1))
  name=$(basename "$file" .md)
  title=$(field "$file" '# ')
  start=$(field "$file" 'Start URL: ')
  obj=$(objective "$file")
  [ -n "$start" ] || start=$URL
  [ -n "$title" ] || title=$name
  if [ -z "$obj" ]; then
    note "kane-verify: $name has no objective paragraph; skipping it."
    continue
  fi
  note "kane-verify: running $name"
  # Kane gets a working directory of its own, so its .testmuai/ and its
  # evidence packs do not land in the repo.
  out=$(cd "$work" && KANE_CLI_USER_AGENT=fixpoint "$KANE" run "$obj" --agent --headless \
    --url "$start" --timeout "$TIMEOUT" --name "fpself-$name" </dev/null 2>&1)
  code=$?
  case $code in
    0)
      note "kane-verify: passed  $name"
      ;;
    *)
      failed=$((failed + 1))
      reason=$(printf '%s' "$out" | grep -o '"reason":"[^"]*"' | sed -n '$s/"reason":"//p' | tr -d '"')
      [ -n "$reason" ] || reason="Kane CLI exited $code"
      tail=$(printf '%s' "$out" | tail -n 12)
      note "kane-verify: FAILED  $name ($reason)"
      note "$tail"
      failures=$(printf '%s\n- %s (%s)\n  objective: %s\n  Kane said: %s\n' \
        "$failures" "$title" "$name" "$obj" "$reason")
      ;;
  esac
done

if [ "$failed" -eq 0 ]; then
  note "kane-verify: $total of $total checks passed."
  rm -f "$counter"
  allow
fi

if [ "$hook" -eq 0 ]; then
  note "kane-verify: $failed of $total checks failed."
  exit 1
fi

blocks=0
[ -f "$counter" ] && blocks=$(cat "$counter" 2>/dev/null || echo 0)
case $blocks in
  '' | *[!0-9]*) blocks=0 ;;
esac
if [ "$blocks" -ge "$MAX_BLOCKS" ]; then
  note "kane-verify: already blocked $blocks times for this session; letting it through. $failed of $total checks still fail."
  rm -f "$counter"
  allow
fi
printf '%s' "$((blocks + 1))" >"$counter"

# json <text> escapes text for a JSON string value.
json() {
  printf '%s' "$1" | awk '
    BEGIN { ORS = "" }
    {
      gsub(/\\/, "\\\\")
      gsub(/"/, "\\\"")
      gsub(/\t/, "\\t")
      if (NR > 1) print "\\n"
      print
    }'
}

reason_text=$(printf "The Kane checks of Fixpoint's own console do not pass, so this work is not done. %d of %d failed:\n%s\nRun scripts/kane-verify.sh to see the full output, fix the console or the check, then finish. This was block %d of at most %d.\n" \
  "$failed" "$total" "$failures" "$((blocks + 1))" "$MAX_BLOCKS")

printf '{"decision":"block","reason":"%s"}\n' "$(json "$reason_text")"
exit 0
