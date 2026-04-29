# Hermes hook recipe

Hermes uses a Python plugin system rather than shell hooks (CLI 0.10+). This directory ships a minimal plugin that wires:

- `pre_llm_call` — per-turn recall against the user's prompt; injects the result via `{"context": "..."}` (the only hook whose return value Hermes uses for context injection).
- `on_session_start` — loads static rules + user context (logged to stderr; the actual injection happens on the first `pre_llm_call`).

## Installation

Hermes plugins live at `~/.hermes/plugins/<name>/`. To install:

```bash
mkdir -p ~/.hermes/plugins/mega-mem
cp /usr/local/share/mega-mem/hooks/hermes/{plugin.yaml,__init__.py} ~/.hermes/plugins/mega-mem/
hermes plugins enable mega-mem
```

Adjust the source path if you installed mega-mem to a different prefix.

## Configuration

Environment variables read by the plugin (set in your shell or Hermes launch script):

| Var | Default | Purpose |
|---|---|---|
| `MEGAMEM_VAULT_PATH` | (none) | Absolute path to the vault root. Required for `on_session_start` static context loading. |
| `MEGAMEM_RECALL_URL` | `http://127.0.0.1:8111/recall` | mega-mem MCP recall endpoint |
| `MEGAMEM_TOP_K` | `5` | Max recall results per turn |
| `MEGAMEM_TIMEOUT_S` | `3` | HTTP timeout (seconds) |
| `MEGAMEM_HARNESS` | `hermes` | Selects which `rules/<harness>-specific/` subdirectory to load |
| `MEGAMEM_MAX_BYTES` | `50000` | Cap on injected context bytes |

## Toggle

The plugin honors the same machine-local toggle as the Claude Code and Codex hook scripts:

```bash
mega-mem hooks disable    # plugin returns None — no injection, no recall calls
mega-mem hooks enable     # restore
```

## Failure mode

If mega-mem's MCP server is unreachable, recall calls fail silently and the plugin returns `None`. The agent's turn is unaffected.

## Notes

- Hermes plugins are disabled by default after installation. Run `hermes plugins enable mega-mem` once.
- Memory bridging is separate from this plugin — see `mega-mem vault <ref> bridge hermes <scope>` for that side.
- Hermes already auto-loads memory from `~/.hermes/memories/` (which is the bridge target). The plugin adds *recall on top of that* — surfacing relevant cross-vault content based on the current prompt.
