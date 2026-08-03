// Package mcpserver implements Conduit's Knowledge Base MCP server on top of
// the official Model Context Protocol Go SDK
// (github.com/modelcontextprotocol/go-sdk).
//
// It replaces the previous hand-rolled JSON-RPC implementation that lived in
// internal/kb/mcp_server.go and was pinned to protocol revision 2024-11-05. The
// SDK negotiates the current spec revision (2026-07-28) while remaining
// backwards compatible with the older legacy `initialize` handshake, so
// existing AI clients keep working unchanged.
//
// Transport note: the stdio transport owns os.Stdout. Nothing in this package
// (or anything it calls) may write to stdout -- doing so corrupts the protocol
// frame stream. All diagnostics go to stderr via the zerolog global logger,
// which defaults to os.Stderr.
package mcpserver

import (
	"context"
	"database/sql"
	"errors"
	"io"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/rs/zerolog"

	"github.com/simpleflo/conduit/internal/kb"
	"github.com/simpleflo/conduit/internal/observability"
)

const (
	// ServerName is the MCP server implementation name advertised to clients.
	// It matches the name used by the previous hand-rolled server so that
	// client-side configuration and prompt tuning carry over unchanged.
	ServerName = "conduit-kb"

	// ServerVersion is the MCP server implementation version.
	ServerVersion = "1.0.0"
)

// Server is the Conduit Knowledge Base MCP server.
//
// It owns an *mcp.Server from the official SDK with Conduit's tools, resources
// and prompts registered, plus the knowledge-base collaborators the handlers
// need.
type Server struct {
	mcp *mcp.Server

	db          *sql.DB
	source      *kb.SourceManager
	searcher    *kb.Searcher
	hybrid      *kb.HybridSearcher
	kagSearcher *kb.KAGSearcher
	indexer     *kb.Indexer
	logger      zerolog.Logger
}

// New creates a KB MCP server.
//
// If hybrid is nil, a HybridSearcher backed by FTS5 only is created, matching
// the behavior of the previous kb.NewMCPServer.
func New(db *sql.DB, hybrid *kb.HybridSearcher) *Server {
	searcher := kb.NewSearcher(db)

	// If no hybrid searcher provided, create one with just FTS5.
	if hybrid == nil {
		hybrid = kb.NewHybridSearcher(searcher, nil)
	}

	s := &Server{
		db:     db,
		source: kb.NewSourceManager(db),
		// searcher backs kb_lexical_search: a direct, un-fused BM25 path.
		searcher: searcher,
		hybrid:   hybrid,
		// KAG searcher (uses SQLite by default, can connect to FalkorDB later).
		kagSearcher: kb.NewKAGSearcher(db, nil),
		indexer:     kb.NewIndexer(db),
		logger:      observability.Logger("kb.mcp"),
	}

	s.mcp = mcp.NewServer(
		&mcp.Implementation{Name: ServerName, Version: ServerVersion},
		&mcp.ServerOptions{
			// Start from empty capabilities so the SDK's historical default of
			// advertising "logging" is not inherited: the previous server never
			// advertised or implemented logging, and the logging feature is
			// deprecated as of spec revision 2026-07-28. The tools, resources
			// and prompts capabilities are still inferred from the features
			// registered below.
			Capabilities: &mcp.ServerCapabilities{},
			// Logger is deliberately nil. A non-nil slog logger would make the
			// SDK emit its own diagnostics; we keep all logging on the zerolog
			// stderr logger to guarantee stdout stays protocol-pure.
			Logger: nil,
		},
	)

	s.registerTools()
	s.registerResources()
	s.registerPrompts()

	return s
}

// MCPServer exposes the underlying SDK server. It is primarily useful for
// tests and for callers that need to attach a non-stdio transport.
func (s *Server) MCPServer() *mcp.Server {
	return s.mcp
}

// Connect connects the server to a single transport and returns the resulting
// session. Used by tests (in-process pipes) and by embedders.
func (s *Server) Connect(ctx context.Context, t mcp.Transport) (*mcp.ServerSession, error) {
	return s.mcp.Connect(ctx, t, nil)
}

// Run serves the MCP protocol over stdin/stdout until the client disconnects
// or ctx is cancelled.
//
// A client hanging up (stdin EOF, closed connection) and a cancelled context
// are normal shutdowns and return nil, matching the previous server, which
// returned nil on io.EOF. Returning an error there would make `conduit mcp kb`
// exit non-zero and print a spurious error every time an AI client detaches.
//
// Everything this method logs goes to stderr; stdout carries protocol frames
// only.
func (s *Server) Run(ctx context.Context) error {
	s.logger.Info().Msg("KB MCP server starting")

	err := s.mcp.Run(ctx, &mcp.StdioTransport{})
	if isCleanShutdown(ctx, err) {
		s.logger.Info().Msg("KB MCP server stopped")
		return nil
	}
	return err
}

// isCleanShutdown reports whether err represents an ordinary end of session
// rather than a failure.
func isCleanShutdown(ctx context.Context, err error) bool {
	switch {
	case err == nil:
		return true
	case errors.Is(err, io.EOF):
		return true
	case errors.Is(err, mcp.ErrConnectionClosed):
		return true
	case errors.Is(err, context.Canceled) && ctx.Err() != nil:
		return true
	default:
		return false
	}
}
