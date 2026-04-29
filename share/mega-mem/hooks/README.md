# mega-mem hook recipes

These are reference hook scripts for wiring mega-mem's `recall` into Claude Code and Codex. Drop them into your harness's hooks config and edit the shebang/path/port to match your setup.

## How it works

Both harnesses expose hook events that can inject additional context into the conversation:

| Event | Triggered when | Use it for |
|---|---|---|
| `SessionStart` | At the start of each session | Loading static context: `rules/shared/`, `rules/<harness>-specific/`, `user/` |
| `UserPromptSubmit` | Each time the user submits a prompt | Query-relevant recall against the user's prompt |

The scripts here call the mega-mem MCP server's recall endpoint over HTTP and return the results in the format the harness expects (plain markdown or JSON with `additional_context`).

## Per-harness setup

- [Claude Code](./claude-code/README.md) — uses `~/.claude/settings.json` hooks block (shell scripts)
- [Codex](./codex/README.md) — uses `~/.codex/hooks.json` or `[hooks]` in `~/.codex/config.toml` (shell scripts)
- [Hermes](./hermes/README.md) — uses Python plugin at `~/.hermes/plugins/mega-mem/`
- OpenClaw — no hook recipe needed. OpenClaw has its own internal `boot-md` hook plus `memorySearch`, both of which read the workspace markdown directly. Bridging the workspace memory subdir is sufficient; mega-mem's MCP server can also be added to OpenClaw's `mcp.servers` for cross-vault recall.

## Prerequisites

- A registered, initialized vault: `mega-mem vaults register <alias> [<path>]` then `mega-mem vault <alias> init`.
- The MCP server running: `mega-mem vault <alias> serve`.
- The vault bridged for the harness you want to wire: `mega-mem vault <alias> bridge claude-code <slug> --apply` or `--apply codex <scope>`.

The hook scripts assume the MCP server listens on `127.0.0.1:8111` (mega-mem's default bind). Adjust the URL in the scripts if your engine config sets a different port.
