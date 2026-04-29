#!/usr/bin/env bash
# Claude Code UserPromptSubmit hook — call mega-mem's recall against the
# user's prompt and emit the top-N results as additionalContext.
#
# Hook input arrives on stdin as JSON. We extract the prompt field, then
# query mega-mem's recall endpoint and print the response as plain markdown.
#
# Required env (with sensible defaults):
#   MEGAMEM_RECALL_URL   recall endpoint (default: http://127.0.0.1:8111/recall)
#   MEGAMEM_TOP_K        max results (default: 5)
#   MEGAMEM_TIMEOUT_S    curl timeout in seconds (default: 3)
#
# This hook fails open: if recall is unreachable, the hook prints nothing
# and exits 0 so the session is not blocked.

set -euo pipefail

# Honor the machine-local toggle written by `mega-mem hooks {enable,disable}`.
state_file="${XDG_CONFIG_HOME:-$HOME/.config}/mega-mem/state.yaml"
if [[ -f "$state_file" ]] && grep -qE '^hooks_enabled:[[:space:]]*false[[:space:]]*$' "$state_file"; then
  exit 0
fi

url="${MEGAMEM_RECALL_URL:-http://127.0.0.1:8111/recall}"
top_k="${MEGAMEM_TOP_K:-5}"
timeout_s="${MEGAMEM_TIMEOUT_S:-3}"

# Extract the prompt from the hook input JSON. Falls back to empty string
# if jq is missing or the field is absent.
prompt=""
if command -v jq >/dev/null 2>&1; then
  prompt=$(jq -r '.prompt // .userPrompt // empty' 2>/dev/null || true)
fi

if [[ -z "$prompt" ]]; then
  exit 0
fi

response=$(curl -fsSG --max-time "$timeout_s" \
  --data-urlencode "q=$prompt" \
  --data-urlencode "top_k=$top_k" \
  "$url" 2>/dev/null || true)

if [[ -z "$response" ]]; then
  exit 0
fi

printf '# Relevant memory (top %s)\n\n%s\n' "$top_k" "$response"
