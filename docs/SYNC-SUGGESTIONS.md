# Cross-machine sync recipes

mega-mem stores everything as plain markdown on disk plus a lightweight SQLite index alongside it. Cross-machine sync is delegated to whatever you already use — there's no mega-mem daemon involved. The vault is just a directory.

This doc collects working recipes per scenario. Pick one; don't combine.

## What lives where

```
<vault>/
  agent-memory/, rules/, user/, ...    # markdown content (sync this)
  .mega-mem/
    index.sqlite                        # search index (sync this if you want)
    cache/                              # embedding cache (sync this if you want)
    backups/                            # memory-snapshot output (sync this if you want)
```

The bridge captures memory only — your harness's per-project notes, daily journals, learned facts. Persona files (SOUL.md, IDENTITY.md, USER.md), project guidance (AGENTS.md, CLAUDE.md), and runtime state (state/, scripts/, lockfiles, sessions) deliberately stay outside the vault. If you want those synced too, point Syncthing (or your sync tool) at the harness home directory directly:

| Harness | What's bridged | What's left at original location (sync separately if you want it) |
|---|---|---|
| Claude Code | `~/.claude/projects/<slug>/memory/` | `~/.claude/CLAUDE.md` (project rules), `~/.claude/settings.json` (your config) |
| Codex | `~/.codex/memories/` | `~/.codex/config.toml`, `~/.codex/rules/` |
| Hermes | `~/.hermes/memories/` (MEMORY.md + USER.md) | `~/.hermes/SOUL.md`, `~/.hermes/skills/`, `~/.hermes/config.yaml` |
| OpenClaw | `~/.openclaw/<workspace>/memory/` (daily journals) | `~/.openclaw/<workspace>/{IDENTITY,SOUL,USER,BOOT,...}.md`, runtime state |

By default, **everything in the vault syncs**. The search index lives at `<vault>/.mega-mem/index.sqlite` so a machine that can't run an embedding model locally (e.g., a Raspberry Pi) can still do recall against a pre-computed index that arrived via Syncthing — provided the embedding-at-query step is offloaded to a remote endpoint.

If you don't want to sync the index (different machines using different embedding models, or you want each machine to rebuild from scratch), exclude `.mega-mem/` via your sync tool's ignore patterns. Recipes below show how per tool.

The `mega-mem vault <ref> init --git` flag scaffolds a `.gitignore` that excludes the index sqlite and editor backups from version control — git almost never wants to track regenerable binary blobs even when Syncthing should.

---

## Recipe 1: Syncthing (recommended for most users)

Best fit for personal use across 2–N machines. P2P, no cloud account, runs on Linux/macOS/Windows.

**Setup:**
1. Install Syncthing on each machine (`apt install syncthing`, `brew install syncthing`, etc.).
2. On machine A: open the Syncthing web UI, click "Add Folder," point at your vault directory (default: `~/.local/share/mega-mem/vaults/<alias>/`). Note the Folder ID.
3. On machine B: in the Syncthing web UI, "Add Folder" with the same Folder ID. Point at the path you want the vault on this machine.
4. Add the other machine as a Device on each side and link them.

**Conflict handling:**
- Syncthing creates `.sync-conflict-*.md` files when both sides edit the same file before the next sync. Run `mega-mem vaults check --conflicts` to flag them; resolve by merging by hand or deleting the loser.

**Excluding the index from sync** (optional — only if you want per-machine indexes):
- Add `.mega-mem` to the per-folder `Ignore Patterns` in the Syncthing UI. The markdown content still syncs; the index rebuilds locally on each machine.
- Heterogeneous embedding models: if Machine A uses `nomic-embed-text` and Machine B uses `mxbai-embed-large`, the indexes shouldn't share. Future versions will tag the index by provider/model so co-existence works; until then, exclude the index per-machine.

---

## Recipe 2: VS Code Remote SSH (no sync needed)

If you SSH from a thin client (laptop / Windows PC) into a canonical machine where Claude Code or other harnesses live, you don't need cross-machine sync at all.

**How it works:**
- VS Code's Remote-SSH extension runs the *extension host* on the remote machine. Any extension installed via the Remote-SSH server — including Claude Code's VS Code extension — executes on the remote.
- The Claude Code instance reads `~/.claude/` on the remote, not on your local machine. Your local machine is just a renderer.
- Same applies to `claude` CLI run inside a remote SSH terminal: that process is on the remote.

**Verify:** open VS Code's extensions panel. Any extension labeled "SSH: \<host\>" runs on the remote. "Local — \<OS\>" runs on your local machine. For a single-source-of-truth setup, you want all your AI extensions in the SSH column.

**When this isn't enough:** if you want a *native* AI extension on your local machine too (no SSH), use Recipe 1 (Syncthing) on top.

---

## Recipe 3: Plain git push/pull

For users who already work in git daily. Manual but reliable. Best for vaults that double as project documentation.

**Setup:**
1. `mega-mem vault <ref> init --git` (scaffolds `git init` and a `.gitignore` template).
2. Create a private remote (GitHub, Gitea, self-hosted).
3. `git -C <vault> remote add origin <url> && git -C <vault> push -u origin main`.
4. On other machines: `git clone <url> <vault-path>`, then `mega-mem vaults register <alias> <vault-path>`.

**Daily workflow:**
- Before starting work: `git -C <vault> pull --rebase`.
- After making changes: `git -C <vault> commit -am "<msg>" && git -C <vault> push`.

**Cron snippet** (auto-commit every 30 min, only if there are changes):
```cron
*/30 * * * * cd ~/.local/share/mega-mem/vaults/personal && git diff --quiet || (git add -A && git commit -m "auto $(date -Iminutes)" && git push)
```

mega-mem deliberately does not ship a `snapshot` wrapper for this — the cron one-liner is simpler and you keep full git control.

---

## Recipe 4: Tailscale + sshfs (advanced)

For users who want one-canonical-machine semantics but with a remote-mounted filesystem on the other side. Latency-sensitive; most operations work fine but recall queries may feel slower over WAN.

**Setup:**
```bash
# On the canonical machine:
mega-mem vault <ref> init
mega-mem vault <ref> serve  # MCP server runs here

# On the secondary machine:
sshfs canonical-host:/path/to/vault ~/vault
mega-mem vaults register canonical ~/vault
# But run MCP from the canonical machine — don't try to serve over sshfs.
```

**Caveats:**
- Don't run two `mega-mem serve` processes against the same vault over a network mount; the index will collide on writes.
- Tailscale provides the secure transport; sshfs handles the mount. Substitute Mutagen if you want bidirectional sync semantics.

---

## Recipe 5: Nextcloud (works but conflict-prone)

If you already run a Nextcloud server, the desktop client can sync the vault directory like any other shared folder. Works, but has caveats.

**Caveats:**
- Nextcloud locks files mid-sync; mega-mem's `vaults check --conflicts` flags `*.sync-conflict-*` markers but you may see transient errors during writes.
- The recall indexer's debounce window should be longer than typical Nextcloud sync latency, or the index will churn.

If you have a choice between Syncthing and Nextcloud for a personal vault, Syncthing wins on robustness. Nextcloud is fine if you're already invested in it for other reasons.

---

## Recipe 6: Dropbox (workable, not recommended)

Dropbox can sync the vault directory but is a worse fit than Syncthing or Nextcloud for two reasons specific to mega-mem:

1. **Conflict marker format.** Dropbox creates files like `<name> (<machine>'s conflicted copy <date>).md` rather than the standardized `.sync-conflict-*` infix Syncthing uses. `mega-mem vaults check --conflicts` flags both patterns, but Dropbox's filenames are noisier and harder to script around.
2. **Slow file events.** Dropbox's filesystem watcher fires events later than Syncthing's, which can cause the recall indexer to see partial writes. Workaround: bump the indexer's debounce window in the engine config (when that knob lands; for now the default debounce is fine for files of typical size).

If you must use Dropbox:
- Put the vault inside the Dropbox folder. The vault is self-contained — `.mega-mem/` lives next to your markdown, so Dropbox carries the index along automatically (useful if one of your machines can't run embeddings locally).
- If the index churn becomes annoying, add `.mega-mem` to Dropbox's selective-sync exclusion so each machine has its own index. The markdown still syncs.

If you don't already use Dropbox, use Syncthing.

---

## Conflict handling

Run `mega-mem vaults check --conflicts` periodically (or after a sync). It scans for:
- `.sync-conflict-*.md` (Syncthing)
- `*.sync-conflict-*` markers (Nextcloud)
- `* conflicted copy *` (Dropbox)
- `*.orig` from git merges
- `*.swp` / `*~` editor backups left in the vault

Resolution is manual — merge by hand or delete the loser. mega-mem doesn't auto-resolve; the content is yours.

---

## Recipe 7: iCloud Drive (Mac-only)

If all your machines are Macs (or Macs + iOS), iCloud Drive is a low-friction option. The vault directory under `~/Library/Mobile Documents/com~apple~CloudDocs/<vault-name>/` syncs across your Apple devices automatically.

**Setup:**
1. `mega-mem vaults register personal ~/Library/Mobile\ Documents/com~apple~CloudDocs/mega-mem-vault`.
2. `mega-mem vault personal init`.
3. Sign in to iCloud Drive on each Mac you want the vault on.

**Caveats:**
- **Mac/iOS only.** No Linux or Windows client. If any of your machines run Linux or Windows, use Syncthing instead.
- iCloud Drive's "Optimized Storage" can evict files you haven't accessed recently. For an active vault this is rarely a problem, but if recall stops finding old notes on a machine, check whether the file is `.icloud` (placeholder, not yet downloaded) and re-download.
- iCloud's conflict format isn't standardized in a way `vaults check --conflicts` catches reliably. Apple sometimes silently merges, sometimes leaves duplicates without a marker. Audit periodically.

If Apple's ecosystem coverage matches your devices, iCloud Drive Just Works. If not, prefer Syncthing.

---

## What doesn't work well

- **Two `serve` processes against the same vault on different machines simultaneously.** The on-disk markdown stays consistent, but indexes drift independently if both processes are writing to `.mega-mem/index.sqlite`. Workaround: only one machine runs `serve`; others use the synced markdown for filesystem access and the running machine's MCP for recall over the network.
