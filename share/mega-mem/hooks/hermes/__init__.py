"""mega-mem plugin for Hermes.

Wires two hooks:

- pre_llm_call: per-turn recall against the user's prompt. Injects results
  via {"context": "..."} so Hermes prepends them to the LLM call.
- on_session_start: loads static context (rules/shared/, rules/hermes-specific/,
  user/) once at session start. Side-effect logging only — Hermes ignores
  on_session_start return values.

Honors the machine-local toggle written by `mega-mem hooks {enable,disable}`
at $XDG_CONFIG_HOME/mega-mem/state.yaml. When disabled, hooks return None
(no injection) without touching anything else.

Configuration via environment variables (set in your shell or your Hermes
launch script):

    MEGAMEM_VAULT_PATH    Absolute path to the mega-mem vault root (required
                          for on_session_start; pre_llm_call works without it
                          if MEGAMEM_RECALL_URL is set).
    MEGAMEM_RECALL_URL    URL of the mega-mem MCP recall endpoint
                          (default: http://127.0.0.1:8111/recall).
    MEGAMEM_TOP_K         Max recall results per turn (default: 5).
    MEGAMEM_TIMEOUT_S     HTTP timeout in seconds (default: 3).
    MEGAMEM_HARNESS       Identifier for the rules/<harness>-specific/
                          subdir to load (default: hermes).
    MEGAMEM_MAX_BYTES     Cap on injected context bytes (default: 50000).
"""

import os
import urllib.parse
import urllib.request


def _toggle_disabled() -> bool:
    """Return True if the machine-local mega-mem toggle is set to disabled."""
    xdg = os.environ.get("XDG_CONFIG_HOME") or os.path.expanduser("~/.config")
    state_file = os.path.join(xdg, "mega-mem", "state.yaml")
    try:
        with open(state_file, "r", encoding="utf-8") as f:
            for line in f:
                stripped = line.strip()
                if stripped.startswith("hooks_enabled:"):
                    value = stripped.split(":", 1)[1].strip()
                    return value == "false"
    except OSError:
        pass
    return False


def _recall(query: str) -> str:
    """Call mega-mem's recall HTTP endpoint and return markdown content.

    Fails open: returns "" on any error so the agent isn't blocked when
    mega-mem isn't running.
    """
    url = os.environ.get("MEGAMEM_RECALL_URL", "http://127.0.0.1:8111/recall")
    top_k = os.environ.get("MEGAMEM_TOP_K", "5")
    timeout = float(os.environ.get("MEGAMEM_TIMEOUT_S", "3"))

    qs = urllib.parse.urlencode({"q": query, "top_k": top_k})
    full = f"{url}?{qs}"
    try:
        with urllib.request.urlopen(full, timeout=timeout) as resp:
            return resp.read().decode("utf-8", errors="replace")
    except Exception:
        return ""


def _load_static_context() -> str:
    """Concatenate markdown from rules/shared/, rules/<harness>-specific/, user/."""
    vault = os.environ.get("MEGAMEM_VAULT_PATH")
    if not vault or not os.path.isdir(vault):
        return ""
    harness = os.environ.get("MEGAMEM_HARNESS", "hermes")
    max_bytes = int(os.environ.get("MEGAMEM_MAX_BYTES", "50000"))

    sources = [
        os.path.join(vault, "rules", "shared"),
        os.path.join(vault, "rules", f"{harness}-specific"),
        os.path.join(vault, "user"),
    ]

    out = [f"# mega-mem context\nLoaded at session start from {vault} for harness {harness}.\n"]
    for src in sources:
        if not os.path.isdir(src):
            continue
        for root, _, files in os.walk(src):
            for fname in sorted(files):
                if not fname.endswith(".md"):
                    continue
                path = os.path.join(root, fname)
                rel = os.path.relpath(path, vault)
                try:
                    with open(path, "r", encoding="utf-8") as f:
                        content = f.read()
                except OSError:
                    continue
                out.append(f"\n## {rel}\n\n{content}")

    joined = "".join(out)
    return joined[:max_bytes]


def pre_llm_call(session_id, user_message, is_first_turn, **kwargs):
    """Per-turn recall. Returns {"context": ...} or None."""
    if _toggle_disabled():
        return None
    if not user_message:
        return None
    body = _recall(user_message)
    if not body:
        return None
    top_k = os.environ.get("MEGAMEM_TOP_K", "5")
    return {"context": f"# Relevant memory (top {top_k})\n\n{body}\n"}


def on_session_start(session_id, **kwargs):
    """Log static context load. Hermes ignores the return value here, so we
    log to stderr in case the user wants to verify the plugin is firing.
    Static context is best delivered via pre_llm_call on the first turn."""
    if _toggle_disabled():
        return None
    body = _load_static_context()
    if body:
        # Print to stderr — observable via `hermes` debug logs without
        # affecting the conversation.
        import sys
        print(f"[mega-mem] loaded {len(body)} bytes of static context for session {session_id}",
              file=sys.stderr)


def register(ctx):
    ctx.register_hook("pre_llm_call", pre_llm_call)
    ctx.register_hook("on_session_start", on_session_start)
