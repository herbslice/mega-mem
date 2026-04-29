# Codex hook recipes

Two scripts (parity with the Claude Code recipes — same backend, same shape):

- [`session-start.sh`](./session-start.sh) — loads `rules/shared/`, `rules/codex-specific/`, and `user/` content from the vault at session start.
- [`user-prompt-submit.sh`](./user-prompt-submit.sh) — calls mega-mem's `recall` against the user's prompt every turn.

## Wiring

Edit `~/.codex/hooks.json` (or merge the snippet into the `[hooks]` section of `~/.codex/config.toml`). A complete example is in [`example-hooks.json`](./example-hooks.json).

```json
{
  "hooks": [
    {
      "event_name": "SessionStart",
      "command": "/usr/local/share/mega-mem/hooks/codex/session-start.sh",
      "env": {
        "MEGAMEM_VAULT_PATH": "/home/you/.local/share/mega-mem/vaults/personal",
        "MEGAMEM_HARNESS": "codex"
      }
    },
    {
      "event_name": "UserPromptSubmit",
      "command": "/usr/local/share/mega-mem/hooks/codex/user-prompt-submit.sh",
      "env": {
        "MEGAMEM_RECALL_URL": "http://127.0.0.1:8111/recall",
        "MEGAMEM_TOP_K": "5"
      }
    }
  ]
}
```

## Output format

Codex command hooks return JSON with `hook_specific_output.additional_context` (string). The scripts here wrap stdout markdown in that envelope automatically.

## Notes

- Codex's "command" handlers are the only ones executed today (CLI 0.125.0). Prompt and async hook handlers are parsed but skipped.
- Codex auto-loads `AGENTS.md` from the working tree independently — that's project guidance, not memory, and is outside mega-mem's concern.
- mega-mem provides the runtime recall layer that's complementary to (not replacing) AGENTS.md.
