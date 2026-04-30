#!/usr/bin/env bash
# Claude Code SessionStart hook — load static rules and user context from the vault.
#
# Concatenates markdown under:
#   $MEGAMEM_VAULT_PATH/rules/shared/
#   $MEGAMEM_VAULT_PATH/rules/<harness>-specific/
#   $MEGAMEM_VAULT_PATH/user/
#
# Output is plain markdown on stdout, which Claude Code attaches to the
# session's system prompt as additionalContext.
#
# Required env:
#   MEGAMEM_VAULT_PATH   absolute path to the vault root
#
# Optional env:
#   MEGAMEM_HARNESS      harness identifier (default: claude-code) — controls
#                        which rules/<harness>-specific/ directory is loaded
#   MEGAMEM_MAX_BYTES    truncate output to this many bytes (default: 50000)

set -euo pipefail

# Honor the machine-local toggle at $XDG_CONFIG_HOME/mega-mem/state.yaml.
# Two layers, kill-switch precedence:
#   1. Global `hooks_enabled: false` disables every harness (hand-edit only).
#   2. Per-harness `hooks: { claude-code: false }` toggled via
#      `mega-mem agents hooks {enable,disable} claude-code`.
# Absent file, absent block, or absent harness key all mean enabled (fail-open).
state_file="${XDG_CONFIG_HOME:-$HOME/.config}/mega-mem/state.yaml"
harness="claude-code"
if [[ -f "$state_file" ]]; then
  # Global kill switch.
  if grep -qE '^hooks_enabled:[[:space:]]*false[[:space:]]*$' "$state_file"; then
    exit 0
  fi
  # Per-harness flag inside the `hooks:` block.
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
    exit 0
  fi
fi

vault="${MEGAMEM_VAULT_PATH:-}"
harness="${MEGAMEM_HARNESS:-claude-code}"
max_bytes="${MEGAMEM_MAX_BYTES:-50000}"

if [[ -z "$vault" ]]; then
  echo "<!-- mega-mem session-start hook: MEGAMEM_VAULT_PATH not set -->" >&2
  exit 0
fi
if [[ ! -d "$vault" ]]; then
  echo "<!-- mega-mem session-start hook: vault $vault not found -->" >&2
  exit 0
fi

dirs=(
  "$vault/rules/shared"
  "$vault/rules/${harness}-specific"
  "$vault/user"
)

emit_dir() {
  local d="$1"
  [[ -d "$d" ]] || return 0
  while IFS= read -r -d '' f; do
    printf '\n## %s\n\n' "${f#$vault/}"
    cat "$f"
  done < <(find "$d" -type f -name '*.md' -print0 2>/dev/null | sort -z)
}

{
  printf '# mega-mem context\n'
  printf 'Loaded at session start from %s for harness %s.\n' "$vault" "$harness"
  for d in "${dirs[@]}"; do
    emit_dir "$d"
  done
} | head -c "$max_bytes"
