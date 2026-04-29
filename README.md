# mega-mem

Memory that travels with you, whatever tools you use. Switch easily between agent frameworks.  Markdown on disk. MCP for agents. No vendor lock-in.

mega-mem is a personal knowledge base + TODO system that decouples your agent context from your harness. Any MCP-speaking agent — Claude Code, OpenClaw, Cursor, raw API — reads and writes the same plain-markdown vault. Switch harnesses, keep your rules, memory, and notes.

## How it works

- **One vault, every agent.** A markdown vault is the source of truth. Any MCP client hits the same tools — `recall`, `get_note`, `create_note` — from Claude Code, OpenClaw, Codex, Cursor, or your own scripts.
- **Bridge harness-native memory.** `mega-mem vault <ref> bridge <harness> <scope>` redirects each harness's auto-memory to live under `agent-memory/<harness>/<scope>/` in the vault — config-redirect for Claude Code (`autoMemoryDirectory` setting), directory symlink for Codex/Hermes/OpenClaw. Personas, identity files, and runtime state stay at their original locations — the bridge is memory-only by design. `unbridge` reverses cleanly for uninstall.
- **Recall via hook.** A short `UserPromptSubmit` / `SessionStart` hook script per harness calls `recall` over HTTP and injects results as `additionalContext`. Same backend, both Claude Code and Codex; OpenClaw can run side-by-side with its built-in memorySearch.
- **Cross-machine sync is your existing tool's job.** Syncthing / VS Code Remote SSH / git push-pull / Nextcloud / Dropbox handle the actual sync. The vault includes its `.mega-mem/index.sqlite`, so a low-power machine (Raspberry Pi, phone) that can't run an embedding model locally can still do recall against a pre-computed index that arrived via sync. See [`docs/SYNC-SUGGESTIONS.md`](./docs/SYNC-SUGGESTIONS.md).
- **By-source layout.** Memories segregate by harness automatically (`agent-memory/claude-code/`, `agent-memory/codex/`, `agent-memory/openclaw/`). Audience filtering is path-based — `rules/shared/` loads everywhere, `rules/<harness>-specific/` loads only for that harness. No `audience:` frontmatter required for the common cases.
- **Canonical rules in markdown.** Cross-harness rules live under `rules/` as plain markdown — bring your own frontmatter conventions or none at all.
- **Plumbing, not an app.** No UI, no hosted service, no proprietary store. Edit in any markdown editor; serve to any agent. Project guidance files (AGENTS.md, CLAUDE.md) are explicitly *not* mega-mem's concern — those belong in your project repo.

## Status

Pre-alpha. CLI, scaffolding, vault management, and CRUD MCP tools work today. The `recall` tool (semantic + lexical hybrid), the bridge / unbridge commands, and the per-harness hook recipes (Claude Code + Codex) — the pieces that close the cross-harness demo loop — are the v1 cutoff. See [`FEATURES.md`](./FEATURES.md) for the full roadmap and [`AGENTS.md`](./AGENTS.md) for contributor rules.

## Stack

- **Language**: Go — single-binary distribution via GoReleaser (planned)
- **Embeddings**: Ollama by default (planned); pluggable via HTTP interface in v2
- **Vector store**: sqlite-vec (planned)
- **Lexical search**: ripgrep (planned)

## Quick start

Every vault is addressed by an alias. Register once, then operate by name:

```sh
# register an alias — path defaults to ~/.local/share/mega-mem/vaults/mykb/
mega-mem vaults register mykb

# ...or register with a specific path
mega-mem vaults register mykb ~/knowledge

# scaffold the vault's initial layout from the vault-root template
mega-mem vault mykb init

# add an org by scaffolding the 'org' template at a subpath...
mega-mem vault mykb scaffold org orgs/example

# ...or create the folder first and let scaffold reconcile the whole vault
mkdir -p ~/knowledge/orgs/example
mega-mem vault mykb scaffold

# preview without writing
mega-mem vault mykb scaffold --dry-run
mega-mem vault mykb scaffold --diff      # also reports extras

# inspect what templates are available and what they expand to
mega-mem template list
mega-mem template show vault-root
mega-mem template show org --format yaml

# run the MCP server (loads ~/.config/mega-mem/engines/mykb.yaml by default)
mega-mem vault mykb serve
```

Manage the registry with `mega-mem vaults list | show <alias> | rename <old> <new> | unregister <alias>`. `vaults register` creates the target directory if missing and refuses to overwrite an existing alias unless `--force` is passed.

Health-check one or all registered vaults — useful after deleting a directory by hand or for CI:

```sh
mega-mem vaults check            # statuses: OK | MISSING | NOT_A_DIR | NOT_A_VAULT
mega-mem vaults check --drift    # also flags DRIFT when the vault-root template has missing/extra items
mega-mem vaults check mykb --format json
```

## Custom templates

The bundled templates under `share/mega-mem/templates/default/` are intentionally editable. Put your own `*.yaml` at any of these locations and they'll take priority in the search path:

| Scope | Path | Use for |
|---|---|---|
| One vault | `<vault>/.mega-mem/templates/` | rules specific to a single vault |
| This user | `~/.config/mega-mem/templates/` | your personal overrides |
| This user (data) | `~/.local/share/mega-mem/templates/` | a full bundle you maintain |
| System-wide | `/usr/local/share/mega-mem/templates/` | distributed with your install |
| Dev / ad hoc | `--templates-dir <path>` or `$MEGAMEM_TEMPLATES_DIR` | one-off override |

`mega-mem template path` prints the search path with existence indicators; `mega-mem template sources <name>` shows every location a given template is defined.

A minimal template is just folders:

```yaml
# ~/.config/mega-mem/templates/client.yaml
name: client
description: Substructure for a client engagement folder.
folders:
  - brief
  - deliverables
  - invoices
```

Then `mega-mem vault mykb scaffold client clients/acme` materializes it. Inherit from a shipped template to extend it:

```yaml
name: client
inherit: [org]
folders:
  - contracts
  - deliverables
```

See the shipped templates in `share/mega-mem/templates/default/` for working examples of `inherit:`, brace expansion (`projects/{active,archive,proposed}`), inline/source files, and `children:` recursion.

## Design overview

- **Layout is template-driven**, not hardcoded. `init` materializes the `vault-root` template (or any name via `--root-template`); `scaffold` applies templates to subpaths or reconciles the whole vault via `children:` recursion. Every template ships as an editable YAML file, not an embedded default.
- **Templates** declare folder structure with inheritance (`inherit:`), Bash-style brace expansion (`orgs/{shared,personal}`), optional files (`files:` with `source:` or `content:` and an `on_conflict` policy), and recursion rules (`children:` — scan a parent directory and apply a named template to each subdir).
- **Configs split by locality**. Machine-local engine config at `~/.config/mega-mem/engines/<alias>.yaml`; vault-local self-describing config at `<vault>/.mega-mem.yaml`. Engine points at vault; vault describes itself.
- **Agent integration via symlinks** (see "How it works"). The `agent-memory/` subtree links to harness-native memory directories; `rules/` holds canonical cross-harness rules.
- **Idempotent scaffolding**. Folders that exist: no-op. Files with matching content: no-op. Files with differing content: skip with stderr warning and exit code 3 (or `--force` to overwrite). `--diff` also reports items in the target that aren't declared by the template.
- **Template resolution** walks a search path (first hit wins): `--templates-dir` flag → `$MEGAMEM_TEMPLATES_DIR` → `<vault>/.mega-mem/templates/` → `$XDG_CONFIG_HOME/mega-mem/templates/` → `$XDG_DATA_HOME/mega-mem/templates/` → `/usr/local/share/mega-mem/templates/` → exe-relative fallback. Per-vault overrides Just Work.
- **Single vault per process**. Multi-vault = multiple processes, each with its own engine config and port. A `mega-mem-fleet` supervisor for running many is planned (see `FEATURES.md` v1.x).

## License

Apache 2.0. See [`LICENSE`](./LICENSE).
