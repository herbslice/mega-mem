// Package server hosts the MCP server exposing vault operations as tools.
package server

import (
	"context"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/herbslice/mega-mem/internal/config"
)

// Server wires the MCP server with the engine + vault configs.
type Server struct {
	engine *config.Engine
	vault  *config.Vault
	mcp    *mcpserver.MCPServer
}

// New constructs a Server and registers its tools.
func New(engine *config.Engine, vault *config.Vault) (*Server, error) {
	s := &Server{
		engine: engine,
		vault:  vault,
		mcp: mcpserver.NewMCPServer(
			"mega-mem",
			"0.0.0",
			mcpserver.WithToolCapabilities(false),
		),
	}
	s.registerTools()
	return s, nil
}

// Run starts the HTTP/SSE transport. Blocks until the server exits.
func (s *Server) Run(_ context.Context) error {
	fmt.Printf("mega-mem listening on %s (vault: %s)\n", s.engine.Bind, s.engine.VaultPath)
	httpSrv := mcpserver.NewSSEServer(s.mcp)
	return httpSrv.Start(s.engine.Bind)
}

func (s *Server) registerTools() {
	// Reads
	s.mcp.AddTool(
		mcp.NewTool("list_notes",
			mcp.WithDescription("List markdown notes in a vault folder. Returns paths relative to vault root."),
			mcp.WithString("folder", mcp.Description("Folder path relative to vault root. Empty string lists the whole vault.")),
		),
		s.listNotesHandler,
	)
	s.mcp.AddTool(
		mcp.NewTool("get_note",
			mcp.WithDescription("Read a single markdown note from the vault by its relative path."),
			mcp.WithString("path", mcp.Description("Path relative to vault root (e.g., 'orgs/myorg/notes/example.md')."), mcp.Required()),
		),
		s.getNoteHandler,
	)

	// Writes
	s.mcp.AddTool(
		mcp.NewTool("create_note",
			mcp.WithDescription("Create a new markdown note. Fails if the file already exists."),
			mcp.WithString("path", mcp.Description("Path relative to vault root."), mcp.Required()),
			mcp.WithString("content", mcp.Description("Full note content including frontmatter."), mcp.Required()),
		),
		s.createNoteHandler,
	)
	s.mcp.AddTool(
		mcp.NewTool("update_note",
			mcp.WithDescription("Replace an existing note's content in full. Fails if the file does not exist."),
			mcp.WithString("path", mcp.Description("Path relative to vault root."), mcp.Required()),
			mcp.WithString("content", mcp.Description("New full content."), mcp.Required()),
		),
		s.updateNoteHandler,
	)
	s.mcp.AddTool(
		mcp.NewTool("patch_note",
			mcp.WithDescription("Append content to the end of an existing note. Fails if the file does not exist."),
			mcp.WithString("path", mcp.Description("Path relative to vault root."), mcp.Required()),
			mcp.WithString("content", mcp.Description("Content to append."), mcp.Required()),
		),
		s.patchNoteHandler,
	)
	s.mcp.AddTool(
		mcp.NewTool("delete_note",
			mcp.WithDescription("Delete a note from the vault. Fails if the file does not exist."),
			mcp.WithString("path", mcp.Description("Path relative to vault root."), mcp.Required()),
		),
		s.deleteNoteHandler,
	)

	// Folder operations
	s.mcp.AddTool(
		mcp.NewTool("create_folder",
			mcp.WithDescription("Create a folder inside the vault (idempotent; safe if it already exists)."),
			mcp.WithString("path", mcp.Description("Folder path relative to vault root."), mcp.Required()),
		),
		s.createFolderHandler,
	)
	s.mcp.AddTool(
		mcp.NewTool("delete_folder",
			mcp.WithDescription("Delete an empty folder from the vault. Fails if the folder is not empty; delete its contents first."),
			mcp.WithString("path", mcp.Description("Folder path relative to vault root."), mcp.Required()),
		),
		s.deleteFolderHandler,
	)

	// Recall (pre-alpha stub — full implementation lands with v0.1.0).
	// Registered so MCP clients see the tool advertised; the handler
	// returns a placeholder explaining the feature is not yet built.
	s.mcp.AddTool(
		mcp.NewTool("recall",
			mcp.WithDescription("Hybrid semantic + lexical search over the vault. PRE-ALPHA STUB: returns a placeholder; semantic recall ships with v0.1.0."),
			mcp.WithString("query", mcp.Description("Free-text query to search the vault for."), mcp.Required()),
			mcp.WithString("top_k", mcp.Description("Maximum number of results to return (default: 5).")),
		),
		s.recallStubHandler,
	)
}
