#!/usr/bin/env bash
# Codex SessionStart hook — load static rules and user context from the vault.
#
# Codex command hooks return JSON with `hook_specific_output.additional_context`
# (string). This script collects markdown from the vault's rules/shared/,
# rules/codex-specific/, and user/ trees, then wraps the concatenated content
# in the expected JSON envelope.
#
# Required env:
#   MEGAMEM_VAULT_PATH   absolute path to the vault root
#
# Optional env:
#   MEGAMEM_HARNESS      harness identifier (default: codex)
#   MEGAMEM_MAX_BYTES    truncate context to this many bytes (default: 50000)

set -euo pipefail

emit_envelope() {
  local body="$1"
  if command -v jq >/dev/null 2>&1; then
    jq -nc --arg ctx "$body" '{hook_specific_output: {additional_context: $ctx}}'
  else
    # Fallback: hand-quoted JSON. Safe for typical markdown payloads.
    printf '{"hook_specific_output":{"additional_context":%s}}\n' \
      "$(printf '%s' "$body" | python3 -c 'import json,sys; print(json.dumps(sys.stdin.read()))' 2>/dev/null || printf '"%s"' "$body")"
  fi
}

# Honor the machine-local toggle at $XDG_CONFIG_HOME/mega-mem/state.yaml.
# Two layers, kill-switch precedence:
#   1. Global `hooks_enabled: false` disables every harness (hand-edit only).
#   2. Per-harness `hooks: { codex: false }` toggled via
#      `mega-mem agents hooks {enable,disable} codex`.
# Absent file, absent block, or absent harness key all mean enabled (fail-open).
state_file="${XDG_CONFIG_HOME:-$HOME/.config}/mega-mem/state.yaml"
harness="codex"
if [[ -f "$state_file" ]]; then
  # Global kill switch.
  if grep -qE '^hooks_enabled:[[:space:]]*false[[:space:]]*$' "$state_file"; then
    emit_envelope ""
    exit 0
  fi
  value=$(awk -v h="$harness" '
    /^hooks:[[:space:]]*$/ { in_hooks=1; next }
    /^[^[:space:]]/ && in_hooks { in_hooks=0 }
    in_hooks && match($0, "^[[:space:]]+" h ":[[:space:]]*(true|false)[[:space:]]*$") {
      val = substr($0, RSTART, RLENGTH)
      sub("^[[:space:]]+" h ":[[:space:]]*", "", val)
      sub("[[:space:]]*$", "", val)
      print val
      exit
    }
  ' "$state_file")
  if [[ "$value" == "false" ]]; then
    emit_envelope ""
    exit 0
  fi
fi

vault="${MEGAMEM_VAULT_PATH:-}"
harness="${MEGAMEM_HARNESS:-codex}"
max_bytes="${MEGAMEM_MAX_BYTES:-50000}"

if [[ -z "$vault" || ! -d "$vault" ]]; then
  emit_envelope ""
  exit 0
fi

dirs=(
  "$vault/rules/shared"
  "$vault/rules/${harness}-specific"
  "$vault/user"
)

collect() {
  printf '# mega-mem context\n'
  printf 'Loaded at session start from %s for harness %s.\n' "$vault" "$harness"
  for d in "${dirs[@]}"; do
    [[ -d "$d" ]] || continue
    while IFS= read -r -d '' f; do
      printf '\n## %s\n\n' "${f#$vault/}"
      cat "$f"
    done < <(find "$d" -type f -name '*.md' -print0 2>/dev/null | sort -z)
  done
}

body=$(collect | head -c "$max_bytes")
emit_envelope "$body"
