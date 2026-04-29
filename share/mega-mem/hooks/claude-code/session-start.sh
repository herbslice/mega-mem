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

# Honor the machine-local toggle written by `mega-mem hooks {enable,disable}`.
# Absent file or absent field means hooks are enabled.
state_file="${XDG_CONFIG_HOME:-$HOME/.config}/mega-mem/state.yaml"
if [[ -f "$state_file" ]] && grep -qE '^hooks_enabled:[[:space:]]*false[[:space:]]*$' "$state_file"; then
  exit 0
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
