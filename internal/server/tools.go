package server

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
)

// --- Reads ---

func (s *Server) listNotesHandler(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.Params.Arguments
	folder, _ := args["folder"].(string)

	base, err := resolveWithinVault(s.engine.VaultPath, folder)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}

	info, err := os.Stat(base)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return mcp.NewToolResultError(fmt.Sprintf("folder not found: %s", folder)), nil
		}
		return nil, fmt.Errorf("stat %s: %w", base, err)
	}
	if !info.IsDir() {
		return mcp.NewToolResultError(fmt.Sprintf("not a folder: %s", folder)), nil
	}

	var notes []string
	walkErr := filepath.WalkDir(base, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if p != base && strings.HasPrefix(name, ".") {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".md") {
			return nil
		}
		rel, err := filepath.Rel(s.engine.VaultPath, p)
		if err != nil {
			return err
		}
		notes = append(notes, filepath.ToSlash(rel))
		return nil
	})
	if walkErr != nil {
		return nil, fmt.Errorf("walk %s: %w", base, walkErr)
	}

	if len(notes) == 0 {
		return mcp.NewToolResultText(""), nil
	}
	return mcp.NewToolResultText(strings.Join(notes, "\n")), nil
}

func (s *Server) getNoteHandler(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.Params.Arguments
	path, ok := args["path"].(string)
	if !ok || path == "" {
		return mcp.NewToolResultError("path is required"), nil
	}
	full, err := resolveWithinVault(s.engine.VaultPath, path)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	data, err := os.ReadFile(full)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return mcp.NewToolResultError(fmt.Sprintf("note not found: %s", path)), nil
		}
		return nil, fmt.Errorf("read %s: %w", full, err)
	}
	return mcp.NewToolResultText(string(data)), nil
}

// --- Writes ---

func (s *Server) createNoteHandler(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	path, content, errResult := notePathAndContent(req)
	if errResult != nil {
		return errResult, nil
	}
	full, err := resolveWithinVault(s.engine.VaultPath, path)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if _, err := os.Stat(full); err == nil {
		return mcp.NewToolResultError(fmt.Sprintf("note already exists: %s", path)), nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("stat %s: %w", full, err)
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return nil, fmt.Errorf("mkdir parent of %s: %w", full, err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		return nil, fmt.Errorf("write %s: %w", full, err)
	}
	return mcp.NewToolResultText(fmt.Sprintf("created: %s", path)), nil
}

func (s *Server) updateNoteHandler(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	path, content, errResult := notePathAndContent(req)
	if errResult != nil {
		return errResult, nil
	}
	full, err := resolveWithinVault(s.engine.VaultPath, path)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if _, err := os.Stat(full); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return mcp.NewToolResultError(fmt.Sprintf("note not found: %s", path)), nil
		}
		return nil, fmt.Errorf("stat %s: %w", full, err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		return nil, fmt.Errorf("write %s: %w", full, err)
	}
	return mcp.NewToolResultText(fmt.Sprintf("updated: %s", path)), nil
}

func (s *Server) patchNoteHandler(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	path, content, errResult := notePathAndContent(req)
	if errResult != nil {
		return errResult, nil
	}
	full, err := resolveWithinVault(s.engine.VaultPath, path)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	f, err := os.OpenFile(full, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return mcp.NewToolResultError(fmt.Sprintf("note not found: %s", path)), nil
		}
		return nil, fmt.Errorf("open %s: %w", full, err)
	}
	defer f.Close()
	if _, err := f.WriteString(content); err != nil {
		return nil, fmt.Errorf("append to %s: %w", full, err)
	}
	return mcp.NewToolResultText(fmt.Sprintf("appended %d bytes to %s", len(content), path)), nil
}

func (s *Server) deleteNoteHandler(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.Params.Arguments
	path, ok := args["path"].(string)
	if !ok || path == "" {
		return mcp.NewToolResultError("path is required"), nil
	}
	full, err := resolveWithinVault(s.engine.VaultPath, path)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if err := os.Remove(full); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return mcp.NewToolResultError(fmt.Sprintf("note not found: %s", path)), nil
		}
		return nil, fmt.Errorf("remove %s: %w", full, err)
	}
	return mcp.NewToolResultText(fmt.Sprintf("deleted: %s", path)), nil
}

// --- Folders ---

func (s *Server) createFolderHandler(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.Params.Arguments
	path, ok := args["path"].(string)
	if !ok || path == "" {
		return mcp.NewToolResultError("path is required"), nil
	}
	full, err := resolveWithinVault(s.engine.VaultPath, path)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if err := os.MkdirAll(full, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir %s: %w", full, err)
	}
	return mcp.NewToolResultText(fmt.Sprintf("created: %s/", path)), nil
}

func (s *Server) deleteFolderHandler(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.Params.Arguments
	path, ok := args["path"].(string)
	if !ok || path == "" {
		return mcp.NewToolResultError("path is required"), nil
	}
	full, err := resolveWithinVault(s.engine.VaultPath, path)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	info, err := os.Stat(full)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return mcp.NewToolResultError(fmt.Sprintf("folder not found: %s", path)), nil
		}
		return nil, fmt.Errorf("stat %s: %w", full, err)
	}
	if !info.IsDir() {
		return mcp.NewToolResultError(fmt.Sprintf("not a folder: %s", path)), nil
	}
	entries, err := os.ReadDir(full)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", full, err)
	}
	if len(entries) > 0 {
		return mcp.NewToolResultError(fmt.Sprintf("folder not empty: %s (delete its contents first)", path)), nil
	}
	if err := os.Remove(full); err != nil {
		return nil, fmt.Errorf("remove %s: %w", full, err)
	}
	return mcp.NewToolResultText(fmt.Sprintf("deleted: %s/", path)), nil
}

// --- Helpers ---

func notePathAndContent(req mcp.CallToolRequest) (path, content string, errResult *mcp.CallToolResult) {
	args := req.Params.Arguments
	path, ok := args["path"].(string)
	if !ok || path == "" {
		return "", "", mcp.NewToolResultError("path is required")
	}
	content, ok = args["content"].(string)
	if !ok {
		return "", "", mcp.NewToolResultError("content is required")
	}
	return path, content, nil
}

// recallStubHandler is the pre-alpha placeholder for the recall tool.
// Returns a friendly message so MCP clients (and any agent that calls it)
// see something useful rather than an empty result. Full implementation —
// Ollama HTTP client + sqlite-vec + ripgrep + hybrid merge — ships with
// v0.1.0; until then `get_note` and `list_notes` cover path-based access.
func (s *Server) recallStubHandler(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.Params.Arguments
	query, _ := args["query"].(string)
	if query == "" {
		return mcp.NewToolResultError("query is required"), nil
	}
	msg := strings.Join([]string{
		"recall is not yet implemented in this pre-alpha build (v0.0.0).",
		"",
		"When v0.1.0 ships, this tool will perform hybrid semantic + lexical search ",
		"over the vault using Ollama embeddings, sqlite-vec, and ripgrep, returning ",
		"top-N matching snippets ranked by relevance.",
		"",
		"For now, navigate the vault directly via `list_notes` and `get_note`.",
		fmt.Sprintf("Query received but not searched: %q", query),
	}, "\n")
	return mcp.NewToolResultText(msg), nil
}

// resolveWithinVault joins a user-supplied relative path to the vault root
// and verifies the result does not escape via "..".
func resolveWithinVault(vaultPath, rel string) (string, error) {
	rel = strings.TrimLeft(rel, "/")
	cleaned := filepath.Clean("/" + rel)[1:]
	full := filepath.Join(vaultPath, cleaned)

	absVault, err := filepath.Abs(vaultPath)
	if err != nil {
		return "", fmt.Errorf("resolve vault path: %w", err)
	}
	absFull, err := filepath.Abs(full)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", full, err)
	}
	if absFull != absVault && !strings.HasPrefix(absFull, absVault+string(filepath.Separator)) {
		return "", fmt.Errorf("path escapes vault: %s", rel)
	}
	return absFull, nil
}
