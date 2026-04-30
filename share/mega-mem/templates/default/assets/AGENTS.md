# Agent rules for this vault

This vault is served by [mega-mem](https://github.com/herbslice/mega-mem) over MCP. Any agent (Claude Code, Codex, Hermes, OpenClaw, raw API) can recall and write here through the shared protocol.

## Primary behavior

- **Recall before raw-reading.** Use the MCP `recall` tool to find relevant notes; do not traverse the whole vault. If you're reading more than a handful of files to answer a question, stop and reach for recall.
- **Prefer editing canonical notes** over creating new sibling files. If you must replace a note, clearly mark the old version as superseded so future readers know which is current.
- **Frontmatter is optional.** mega-mem imposes no schema — bring your own conventions or none at all.

## Layout

The default layout is intentionally flat — substructure is opt-in. The top-level folders that ship by default:

- `agent-memory/` — harness-native memory, populated only when you run `mm agents bridge --memory <harness>`. Each bridged harness gets its own subtree (e.g., `agent-memory/claude-code/projects/`, `agent-memory/codex/memories/`). Empty until then.
- `rules/` — cross-harness behavioral rules. Files here are loaded for every harness; organize by topic with your own subfolders if you want. (Audience-filtered rules are planned via frontmatter; until then, everything in `rules/` is universal.)
- `user/` — cross-cutting personal context (profile, preferences, todos). No imposed substructure.
- `orgs/` — one subfolder per organization. Apply the `org` template (`mm vault <alias> scaffold org orgs/foo`) for the canonical substructure: `infra/`, `projects/`, `decisions/`, `runbooks/`, `todos/`, `notes/`.
- `projects/` — top-level projects for solo users who don't need multi-org segregation. Drop project notes here directly.
- `tools/` — reference material on tools, libraries, languages, frameworks.
- `reference/` — universal technical knowledge that doesn't fit `tools/`. File by topic.
- `people/` — cross-org contacts.
- `inbox/` — fresh captures awaiting filing.

## How harnesses see this vault

Each harness reads the vault contents through MCP `recall`. When `mm agents bridge --memory <harness>` is in effect, the harness's native memory directory lives inside this vault under `agent-memory/<harness>/`, so writes flow back here automatically. Cross-cutting context like `rules/` and `user/` is injected at session start by a mega-mem hook.

## TODOs

Tasks live as Obsidian Tasks syntax inside notes:

```text
- [ ] Something to do 📅 2026-04-30 ⏫
```

Org-specific tasks go in `orgs/<org>/todos/`. Cross-cutting personal tasks go in `user/`. Project-specific tasks live alongside the project notes.

## What not to do

- Do not raw-read large swaths of the vault to answer a question. Use `recall`.
- Do not put machine-specific paths in files that sync (e.g., `.mega-mem.yaml`). Those belong in the engine config at `~/.config/mega-mem/engines/`.
- Do not write project guidance (architecture decisions, coding standards) into vault memory — that belongs in your project's own `AGENTS.md` / `CLAUDE.md` files, not here.
