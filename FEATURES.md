# mega-mem — planned features

Agent-agnostic personal knowledge base + TODO system served over MCP. Any agent (Claude Code, OpenClaw, Cursor, raw API) can recall and write through a shared protocol. Each vault is self-contained with its own config and structure; a single mega-mem deployment can serve multiple segregated vaults as separate processes.

## Design philosophy

mega-mem is plumbing, not an app. The intended split of interfaces:

- **Agents** use the filesystem + MCP (`recall`, `get_note`, CRUD tools). These are the first-class agent surfaces.
- **Humans** use the markdown editor of their choice. Obsidian is the expected common case, but nothing in mega-mem requires it — any editor that handles markdown works.

mega-mem does not ship a UI; it builds the layer that makes any UI work better. Ecosystem projects (an Obsidian plugin, a server-management dashboard) may come later as *separate* projects that consume mega-mem.

## v1 (MVP)

**Core CLI**
- `mega-mem vault <ref> init [--force] [--dry-run] [--scaffold] [--root-template <name>] [--git]` — writes `.mega-mem.yaml` only by default; `--scaffold` (or non-empty `--root-template`) also applies the default `vault-root` template. Designed so adopting an existing Obsidian vault is friction-free.
- `mega-mem vault <ref> scaffold [<template> <subpath>] [--force] [--dry-run] [--diff] [--no-recurse] [--tree] [--format text|json]`
- `mega-mem vault <ref> serve [--config <path>]`
- `mega-mem vault <ref> status`
- `mega-mem vault tell` — print the alias of the registered vault containing the current working directory; exit 4 if not in a vault. Useful in shell scripts and prompts.
- `mega-mem agents bridge <harness> [--vault <a>] [--memory] [--scope <s>] [--mcp-url <u>] [--apply] [--no-mcp] [--list-scopes]` — wires mega-mem's MCP server into the harness's config. **Default = MCP only.** With `--memory`, also redirects the harness's memory directory into the vault: symlink-replace on `~/.claude/projects/` (Claude Code), `~/.codex/memories/` (Codex), `~/.hermes/memories/` (Hermes), or every `~/.openclaw/<ws>/memory/` (OpenClaw). With `--scope`, narrows to one instance. With `--list-scopes`, enumerates discoverable scope names. `--vault` defaults to the only registered vault if there's exactly one.
- `mega-mem agents unbridge <harness> [--vault <a>] [--memory] [--scope <s>] [--apply] [--no-mcp] [--keep-vault]` — reverses a bridge. By default, only removes mega-mem from the harness's MCP-client config; `--memory` also restores the harness's memory directory from the vault.
- `mega-mem agents list [--format text|json]` — discover harness installations on this machine and report bridge state (installed, MCP wired, memory bridged → which vault, hooks on/off).
- `mega-mem agents hooks {enable,disable,status} [<harness>]` — per-harness machine-local toggle for mega-mem's shipped hook scripts. Absent harness key = enabled (fail-open default).
- `mega-mem vaults {list,register,unregister,rename,show,check}` — `check [<alias>] [--drift] [--conflicts] [--format text|json]` verifies registered vaults against the filesystem; `--conflicts` flags Syncthing/Nextcloud sync-conflict files and git merge artifacts.
- `mega-mem templates {list,show,sources,path}` with `--vault <ref>`, `--format text|yaml|json`, `--decorate`. (`template` accepted as a singular alias.)
- Vault ref = alias only, resolved via `~/.config/mega-mem/vaults.yaml`; paths are registered once via `mega-mem vaults register <alias> [<path>]` (defaults to `~/.local/share/mega-mem/vaults/<alias>/`)
- `--templates-dir` persistent flag prepends a directory to the search path

**Templates module**
- User-defined folder structures declared as editable YAML
- Template inheritance via `inherit: [base, ...]`: folder lists merged with parents' (deduped, child overrides on conflicts). Semantics borrowed from GitLab CI's `extends:` — list-valued, multi-parent, later-overrides-earlier
- Brace-expansion syntax for folder specs: `projects/{active,archive,proposed}` expands to three folders; nested expansions supported (`{a,b}/{x,y}`). Follows Bash brace expansion
- Base/overlay mental model from Kustomize: bases are reusable and context-free; overlays stay thin and specific
- Files declared alongside folders via `files: [{path, source|content, mode, on_conflict}]`
- Child recursion via `children: [{parent, template, exclude}]` — scans existing subdirs and applies named template to each
- Filesystem search path (first hit wins): `--templates-dir` → `$MEGAMEM_TEMPLATES_DIR` → `<vault>/.mega-mem/templates/` → `$XDG_CONFIG_HOME/mega-mem/templates/` → `$XDG_DATA_HOME/mega-mem/templates/` → `/usr/local/share/mega-mem/templates/` → `<exe-dir>/../share/mega-mem/templates/default/`
- Design note: no widely-adopted spec exists for folder-tree declaration with inheritance (confirmed by survey of cookiecutter, copier, yeoman, Backstage, mtree, `/etc/skel`, PDS Skeleton). Naming is deliberately borrowed rather than invented

**Scaffold semantics**
- Folders that exist are silent no-ops; folders that don't are created
- Files with identical content are silent no-ops; files with differing content are skipped (warn on stderr, exit 3) unless `--force` or `on_conflict: overwrite`
- `--diff` additionally reports extras (items in target not declared by template) — informational only, never modified
- `scaffold` with no args reconciles the whole vault via the root template and its `children:` chain
- Recursion goes depth-first, skipping names in `exclude:` and dotfiles

**Configuration model**
- `~/.config/mega-mem/vaults.yaml` — alias registry (machine-local)
- `~/.config/mega-mem/engines/<alias>.yaml` — per-vault engine config (bind, embedding, agent-memory)
- `<vault>/.mega-mem.yaml` — in-vault config (travels with vault)
- Machine-specific paths (vault paths, agent-memory source dirs) live in engine config, never vault config

**MCP server**
- Go MCP server over SSE transport (mcp-go v0.24; StreamableHTTP will come when the library ships it)
- Tools: `list_notes`, `get_note`, `create_note`, `update_note`, `patch_note` (append), `delete_note`, `create_folder`, `delete_folder` (non-empty errors)
- Bind modes: localhost, specific interface (e.g., Wireguard), or any-interface

**Exit codes**
- `0` — clean success
- `1` — unexpected error (I/O, config parse, etc.)
- `2` — bad invocation (invalid args, missing required flag) — cobra-native
- `3` — completed with skipped items (conflicts without `--force`)

## v1 (remaining MVP work)

- **`recall` tool** — Ollama HTTP client + sqlite-vec + background indexer + hybrid merge with ripgrep (lexical + semantic). Index lives at `<vault>/.mega-mem/index.sqlite`, gitignored by default but synced with the vault by Syncthing/Nextcloud/etc. so low-power machines (e.g., Raspberry Pi) can do recall against a pre-computed index without local embeddings. Users can exclude `.mega-mem/` from sync via the sync tool's ignore patterns if they want per-machine indexes.
- **`list_tasks` tool** — regex over vault for Obsidian Tasks syntax (`- [ ] ... 📅 DATE`)
- **Bridge / unbridge commands** — `mega-mem agents bridge|unbridge <harness>` per the CLI spec above. `internal/bridge/` package implements per-harness logic; MCP wiring is the default action, memory bridging is opt-in via `--memory`.
- **Hook recipes for Claude Code** (shipped as shell scripts under `share/mega-mem/hooks/claude-code/`):
  - `UserPromptSubmit` — per-turn `recall` against the prompt; returns content as `additionalContext`.
  - `SessionStart` — load `rules/` + `user/` as `additionalContext`.
- **Hook recipes for Codex** (shipped under `share/mega-mem/hooks/codex/` plus a `hooks.json` snippet for `~/.codex/hooks.json`):
  - `UserPromptSubmit` — per-turn `recall`; same backend as Claude Code's hook. Codex command hooks inject via `hook_specific_output.additional_context` (parity with Claude Code).
  - `SessionStart` — load `rules/` + `user/`.
- **Hook recipes for Hermes** (Python plugin under `share/mega-mem/hooks/hermes/`):
  - `pre_llm_call` — per-turn `recall`; injected via `{"context": "..."}`.
  - `on_session_start` — loads static context for observability.
- **Per-harness hook toggle** — `mega-mem agents hooks {enable,disable} [<harness>]` writes `hooks: { <harness>: bool }` in `~/.config/mega-mem/state.yaml`. Hook scripts read the per-harness key at the top of each invocation; absent key = enabled (fail-open). A top-level `hooks_enabled: false` (hand-edit only) acts as a global kill switch: when set, every harness's hooks are disabled regardless of the per-harness map. Useful for one-line silencing across all harnesses, including ones added in the future without explicit per-harness entries.
- **`docs/SYNC-SUGGESTIONS.md`** — cross-machine recipes (Syncthing, VS Code Remote SSH, plain git push/pull, Tailscale + sshfs, conflict handling).
- **GoReleaser config** — linux-amd64/arm64, darwin-amd64/arm64 tarballs + Homebrew tap + optional Docker image; Windows deferred
- **Install script** — `curl -fsSL <url> | bash` for macOS + Linux, OS/arch auto-detect

**Out of scope for v1, v1.x, and v2:**
- Rendering or managing AGENTS.md / CLAUDE.md files. These are *project guidance* (coding standards, architecture, workflows), authored by the user and lived in repo. mega-mem is memory plumbing; it has no opinion on a project's rules files. Users manage their own AGENTS.md and CLAUDE.md.
- Persona / identity bridging (SOUL.md, IDENTITY.md, USER.md as a persona file). These have different lifecycles from memory and overlap with the agentskills.io ecosystem. A future `mega-mem persona bridge` command may revisit this; until then, users manage persona files manually or via Syncthing on the harness home dir.
- KB-app features (tagging UI, wikilink graphs, daily-note templates, web UI). The vault is markdown; users wanting a KB layer point Obsidian at it.
- Non-markdown ingestion (PDF/HTML/image/OCR). Markdown only.

## v1.x (near-term)

**Server and recall**
- `audience:` frontmatter filter on recall — fine-grained gating beyond the path-glob layout that v1 ships
- Per-corpus decay weights: recall relevance adjusted by recency with configurable half-life per folder
- `snapshot` git wrapper (`mega-mem vault <ref> snapshot [--message <msg>]`) — only if users actually ask for it; default lean is to leave git unwrapped, since `git commit -am` from the vault works fine
- Tasks write operations: `add_task`, `complete_task`
- `mega-mem-fleet` supervisor: spawn and manage multiple per-vault processes from a single config
- `mega-mem persona bridge` — separate command for capturing persona / identity files (SOUL.md, IDENTITY.md, OpenClaw workspace markdown) across machines. Designed independently from memory bridging; may integrate with agentskills.io rather than replicating it.

**Obsidian integration (existing-vault use case)**
- Indexer skips `.obsidian/` and auto-detects the attachment directory from `.obsidian/app.json`
- Binary file types (images, PDFs, video) skipped during indexing; in-PDF text extraction deferred to v2
- Extract wikilinks (`[[target]]`, `[[target|alias]]`) into a link graph for graph-augmented recall
- Extract tags (`#category`, nested `#projects/foo`) as first-class filters; `recall` grows a `tags:` parameter
- Respect user-defined frontmatter — mega-mem imposes no schema. (Katamaran is one possible companion for users who want lifecycle tooling on the same vault, but it is not a dependency.)
- Return backlinks alongside `recall` results as metadata
- Verify `list_tasks` against the full Obsidian Tasks plugin grammar (emoji priorities, due/scheduled/recurrence, contexts, projects)
- Wikilinks mode for shared-folder sources (Windows-friendly alternative to symlinks); config-selectable per vault
- Wikilink alias generation: agents inserting long paths use `[[long/path|short]]` form automatically

**Install polish**
- `--decorate` polish: ANSI colors and Unicode tree glyphs for `template show`
- Lazy-download default embedding model on first run: eliminates Ollama as a hard prerequisite for basic operation; Ollama remains the supported upgrade path
- Scoop bucket for Windows once Windows is in scope
- Optional: thin `npm` wrapper for distribution in the Node ecosystem

## Recall depth (v1.x → v2)

Retrieval is the primary value prop; this section collects the tuning surface and the retrieval-quality upgrades worth sequencing in after v1 ships.

**Per-query tuning parameters** — exposed as MCP `recall` tool arguments and as CLI flags on a `recall-test` command:
- `top_k`, `min_score` — basic
- `temperature` — diversity knob via **Maximal Marginal Relevance (MMR)**; 0 = pure similarity, 1 = maximum diversity
- `affinity` — list of tags / audiences / folder prefixes that boost matching results (score multipliers, not hard filters)
- `filters` — hard filters: tag equality, date range, folder prefix; applied before ranking
- `expand_hops` — graph neighborhood expansion via wikilinks (0 = no traversal, 1/2 = N-hop)
- `mode` — preset: `precise` | `balanced` | `exploratory`; maps to combinations of the above

**Per-vault defaults** live under `recall:` in `.mega-mem.yaml`, each query can override.

**Retrieval technique upgrades** (ordered by expected ROI):
1. **HyDE (Hypothetical Document Embeddings)** — LLM generates a hypothetical answer, embed that, search by similarity. Usually beats raw-query embedding for knowledge retrieval; one extra LLM call per query.
2. **Cross-encoder reranking** — retrieve top-100 via embeddings, rerank to top-10 with a cross-encoder (BAAI/bge-reranker family). Biggest precision win on a modest budget.
3. **Multi-vector / section-level embeddings** — one note → multiple embeddings (per heading section). Retrieves paragraph-level matches on long notes.
4. **Query expansion** — rewrite the query into 2–3 variants via the LLM; reciprocal-rank-fusion across searches.
5. **Graph-augmented retrieval** — use wikilinks to extend the retrieved set beyond semantic neighbors. Pairs especially well with Obsidian vaults.
6. **Late chunking** — embed whole note, slice afterward; preserves global context in each chunk.
7. **User feedback loop** — thumbs-up/down on results models as a relevance signal for future queries.

**Tuning UX**: a `mega-mem vault <alias> recall-test "<query>"` CLI command that prints snippets + scores so users can develop intuition for their own vault.

## v2 (medium-term)

- **Per-folder retrieval tuning** — JSON config under `<vault>/.mega-mem/folders.yaml` declaring per-folder `decay_half_life`, `affinity` (boost/deboost multiplier), `temperature` (re-rank looseness), with per-request override at the MCP call site. Enables subagents with different "personalities" (strict vs. creative) over the same data.
- Auto-commit git daemon with debounce (default 30m, configurable up to end-of-session); `POST /flush` for explicit triggers. Deferred from v1.x because the bare `git commit -am` workflow covers most users.
- Embedding provider plugin system with documented HTTP spec; reference plugins for OpenAI, Cohere, Hugging Face Inference
- Bundled in-binary embedding via ONNX Runtime (single-binary install, no first-run download)
- OpenClaw deeper integration: replace OpenClaw's internal memorySearch with the mega-mem MCP (both already use sqlite-vec + Ollama + hybrid; consolidation is straightforward once MCP surfaces the right tunables)
- Multi-tenant auth for hosted multi-vault deployments (API keys, per-vault scoped access)
- Vault health diagnostics: stale notes, orphan files, broken wikilinks, undeclared frontmatter fields
- Native distro packages (deb/rpm via nfpm)

## Ecosystem (separate projects)

Per the design philosophy above, these are projects that *consume* mega-mem — not features of the core binary.

- **Obsidian plugin** — in-vault UI for template operations and scaffolding (what Obsidian is good at: file/folder manipulation, frontmatter edits). Does NOT manage the MCP server — that's a separate concern.
- **Server-management app** — separate tool (systray, web UI, or simple daemon controller) that starts/stops/monitors `mega-mem serve` processes and their vaults. Scope separation: Obsidian plugin = data; management app = runtime.
- **Web UI admin dashboard** — corpus stats, embedding queue health, config + template management
- **Bidirectional sync with Wiki.js / other wiki platforms**
- **Plugin API for non-Obsidian backends** (Logseq, SilverBullet, raw markdown)

## Ideas / future

- Distributed mode: multiple mega-mem instances sharing a vault via operational transform or CRDT
- Export formats: static HTML site, JSON bundle, PDF
- Cross-vault federated recall: single query spans multiple vaults with auth-scoped results
- agentd integration (ai-os): callable as a library + event emission for recall/consolidation/pruning
- **Upstream PR to OpenClaw: user-configurable pre-prompt hook.** OpenClaw currently exposes only `hooks.internal` (built-in, on/off only). A `hooks.user` (or similar) event firing before each LLM call, with stdout concatenated into the system message, would let mega-mem ship an OpenClaw hook recipe matching the Claude Code / Codex / Hermes pattern. Until then, OpenClaw users get auto-recall over their own workspace journals (via the symlink + native memorySearch) plus agent-initiated cross-vault recall via MCP — but no automatic per-turn injection of vault content. Lives outside mega-mem; tracked here so the integration story stays complete.
