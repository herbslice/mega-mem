# Claude Code hook recipes

Two scripts:

- [`session-start.sh`](./session-start.sh) — loads `rules/shared/`, `rules/claude-code-specific/`, and `user/` content from the vault at session start. One-shot per session.
- [`user-prompt-submit.sh`](./user-prompt-submit.sh) — calls mega-mem's `recall` against the user's prompt and injects top-N results as context. Runs every turn.

## Wiring

Edit `~/.claude/settings.json` (or `.claude/settings.local.json`) and add the hooks block. A complete example is in [`example-settings.json`](./example-settings.json).

```json
{
  "hooks": {
    "SessionStart": [
      {
        "command": "/usr/local/share/mega-mem/hooks/claude-code/session-start.sh",
        "env": {
          "MEGAMEM_VAULT_PATH": "/home/you/.local/share/mega-mem/vaults/personal",
          "MEGAMEM_HARNESS": "claude-code"
        }
      }
    ],
    "UserPromptSubmit": [
      {
        "command": "/usr/local/share/mega-mem/hooks/claude-code/user-prompt-submit.sh",
        "env": {
          "MEGAMEM_RECALL_URL": "http://127.0.0.1:8111/recall",
          "MEGAMEM_TOP_K": "5"
        }
      }
    ]
  }
}
```

## What gets injected

The hook output is delivered to Claude as `additionalContext` — content appended to the system prompt for that session (SessionStart) or that turn (UserPromptSubmit). It's additive to whatever Claude Code already loads from `MEMORY.md` and CLAUDE.md files.

## Disabling

Remove the relevant hook entry from `settings.json`. Hooks won't run after the next Claude Code restart.
