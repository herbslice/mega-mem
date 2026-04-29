#!/usr/bin/env bash
# Codex UserPromptSubmit hook — call mega-mem's recall and inject results
# as additional_context.
#
# Codex passes hook input as JSON on stdin (session_id, turn_id, transcript_path,
# cwd, model, plus event-specific fields). For UserPromptSubmit, the prompt
# is in the .prompt field.
#
# Required env (with sensible defaults):
#   MEGAMEM_RECALL_URL   recall endpoint (default: http://127.0.0.1:8111/recall)
#   MEGAMEM_TOP_K        max results (default: 5)
#   MEGAMEM_TIMEOUT_S    curl timeout in seconds (default: 3)
#
# Fails open: if recall is unreachable, returns an empty additional_context
# rather than blocking the turn.

set -euo pipefail

emit_envelope() {
  local body="$1"
  if command -v jq >/dev/null 2>&1; then
    jq -nc --arg ctx "$body" '{hook_specific_output: {additional_context: $ctx}}'
  else
    printf '{"hook_specific_output":{"additional_context":%s}}\n' \
      "$(printf '%s' "$body" | python3 -c 'import json,sys; print(json.dumps(sys.stdin.read()))' 2>/dev/null || printf '""')"
  fi
}

# Honor the machine-local toggle written by `mega-mem hooks {enable,disable}`.
state_file="${XDG_CONFIG_HOME:-$HOME/.config}/mega-mem/state.yaml"
if [[ -f "$state_file" ]] && grep -qE '^hooks_enabled:[[:space:]]*false[[:space:]]*$' "$state_file"; then
  emit_envelope ""
  exit 0
fi

url="${MEGAMEM_RECALL_URL:-http://127.0.0.1:8111/recall}"
top_k="${MEGAMEM_TOP_K:-5}"
timeout_s="${MEGAMEM_TIMEOUT_S:-3}"

prompt=""
if command -v jq >/dev/null 2>&1; then
  prompt=$(jq -r '.prompt // empty' 2>/dev/null || true)
fi

if [[ -z "$prompt" ]]; then
  emit_envelope ""
  exit 0
fi

response=$(curl -fsSG --max-time "$timeout_s" \
  --data-urlencode "q=$prompt" \
  --data-urlencode "top_k=$top_k" \
  "$url" 2>/dev/null || true)

if [[ -z "$response" ]]; then
  emit_envelope ""
  exit 0
fi

body=$(printf '# Relevant memory (top %s)\n\n%s\n' "$top_k" "$response")
emit_envelope "$body"
