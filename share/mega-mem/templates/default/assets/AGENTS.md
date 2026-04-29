# Agent rules for this vault

This vault is served by [mega-mem](https://github.com/herbslice/mega-mem) over MCP. Any agent (Claude Code, OpenClaw, Codex, raw API) can recall and write here through the shared protocol.

## Primary behavior

- **Recall before raw-reading.** Use the MCP `recall` tool to find relevant notes; do not traverse the whole vault. If you're reading more than a handful of files to answer a question, stop and reach for recall.
- **Prefer editing canonical notes** over creating new sibling files. If you must replace a note, clearly mark the old version as superseded so future readers know which is current.
- **Frontmatter is optional.** mega-mem imposes no schema — bring your own conventions or none at all.

## Layout

- `agent-memory/` — harness-native memory, separated by source:
  - `claude-code/<project-slug>/` — bridged from Claude Code's auto-memory dir
  - `codex/<scope>/` — bridged from `~/.codex/memories/`
  - `hermes/<scope>/` — bridged from `~/.hermes/memories/`
  - `openclaw/<workspace>/` — bridged from `~/.openclaw/<workspace>/memory/` (just the daily journals — persona files like SOUL.md, IDENTITY.md, USER.md stay at their original paths)
- `rules/` — agent-agnostic behavioral rules:
  - `shared/` — cross-harness rules loaded by every agent
  - `claude-code-specific/` — only for Claude Code sessions
  - `codex-specific/` — only for Codex sessions
- `user/` — cross-cutting personal context (profile, preferences, todos)
- `orgs/` — one subfolder per organization (`infra/`, `projects/`, `decisions/`, `runbooks/`, `todos/`, `notes/`)
- `orgs/shared/` — resources shared across multiple orgs
- `reference/` — universal technical knowledge (`patterns/`, `tooling/`, `models/`)
- `people/` — cross-org contacts
- `inbox/` — fresh captures awaiting filing

## How harnesses see this vault

Each harness reads its own subtree under `agent-memory/` plus cross-cutting content from `rules/shared/`, `rules/<harness>-specific/`, and `user/` injected at session start by a mega-mem hook. Audience filtering is achieved by directory layout — no `audience:` frontmatter required for the common cases.

## TODOs

Tasks live as Obsidian Tasks syntax inside notes:

```
- [ ] Something to do 📅 2026-04-30 ⏫
```

Org-specific tasks go in `orgs/<org>/todos/`. Cross-cutting personal tasks go in `user/todos/`.

## What not to do

- Do not raw-read large swaths of the vault to answer a question. Use `recall`.
- Do not create new top-level folders without updating the vault template.
- Do not put machine-specific paths in files that sync (e.g., `.mega-mem.yaml`). Those belong in the engine config at `~/.config/mega-mem/engines/`.
- Do not write project guidance (architecture decisions, coding standards) into vault memory — that belongs in your project's own `AGENTS.md` / `CLAUDE.md` files, not here.
