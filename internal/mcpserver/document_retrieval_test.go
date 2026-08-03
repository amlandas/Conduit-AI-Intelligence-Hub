package mcpserver

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Issue #91: search -> read the full document was a broken flow.
//
// The owner watched Claude Code call kb_search successfully, try to fetch the
// document, fail, say "The document ID isn't the path", and fall back to
// reading the file off disk. kb_get_document required an internal document_id;
// the result text printed only a title and a path. A client with filesystem
// access papers over this. Claude Desktop and ChatGPT cannot.
//
// The tests below therefore work the way a real client does: they read the
// document_id out of the RESULT TEXT with a regex rather than reaching into the
// fixture for a known ID. A test that hardcodes "doc-auth" passes even when the
// ID is invisible on the wire, which is exactly the bug.

// documentIDLine matches the retrieval key as it appears in tool output.
var documentIDLine = regexp.MustCompile(`(?m)^document_id: (.+)$`)

// extractDocumentID pulls the first document_id out of tool result text, the
// way a client parsing the transcript would.
func extractDocumentID(t *testing.T, text string) string {
	t.Helper()

	m := documentIDLine.FindStringSubmatch(text)
	if m == nil {
		t.Fatalf("no document_id line in result text -- a client cannot get from a search hit to kb_get_document:\n%s", text)
	}
	return strings.TrimSpace(m[1])
}

// TestSearchToGetDocumentRoundTrip is the owner's exact flow, end to end over
// the protocol: search, take the ID out of the text, fetch the document.
func TestSearchToGetDocumentRoundTrip(t *testing.T) {
	searchTools := []struct {
		name string
		args map[string]any
	}{
		{ToolSearch, map[string]any{"query": "authentication token"}},
		{ToolLexicalSearch, map[string]any{"query": "bearer token"}},
		{ToolSearchWithContext, map[string]any{"query": "authentication token"}},
	}

	for _, tc := range searchTools {
		t.Run(tc.name, func(t *testing.T) {
			cs := connect(t, testServer(t))

			res := callText(t, cs, tc.name, tc.args)
			if res.IsError {
				t.Fatalf("%s reported a tool error: %s", tc.name, resultText(t, res))
			}
			searchText := resultText(t, res)
			if !strings.Contains(searchText, "Authentication Design") {
				t.Fatalf("%s did not surface the expected document:\n%s", tc.name, searchText)
			}

			documentID := extractDocumentID(t, searchText)

			doc := callText(t, cs, ToolGetDocument, map[string]any{"document_id": documentID})
			if doc.IsError {
				t.Fatalf("kb_get_document(%q) failed after %s: %s", documentID, tc.name, resultText(t, doc))
			}
			docText := resultText(t, doc)
			for _, want := range []string{"# Authentication Design", "Path: /corpus/authentication.md", "ErrUnauthorizedToken"} {
				if !strings.Contains(docText, want) {
					t.Errorf("reconstructed document missing %q:\n%s", want, docText)
				}
			}
		})
	}
}

// TestSearchHitFormatIsStableWithDocumentID pins the hit block: the
// document_id line sits directly under Path, and nothing else moved. Scripts
// and tuned client prompts parse this text.
func TestSearchHitFormatIsStableWithDocumentID(t *testing.T) {
	cs := connect(t, testServer(t))

	text := resultText(t, callText(t, cs, ToolSearch, map[string]any{"query": "authentication token"}))
	if !strings.Contains(text, "Path: /corpus/authentication.md\ndocument_id: doc-auth\n") {
		t.Errorf("document_id is not on its own line directly under Path:\n%s", text)
	}
	if !regexp.MustCompile(`\*\*Authentication Design\*\* \(score: -?\d+\.\d{4}\)\nPath: `).MatchString(text) {
		t.Errorf("the title/score header changed shape:\n%s", text)
	}

	// kb_search_with_context carries the key under its citation line. It has no
	// Path: line at all -- Source is a bare filename -- so without document_id
	// that tool is a hard dead end.
	ctxText := resultText(t, callText(t, cs, ToolSearchWithContext, map[string]any{"query": "authentication token"}))
	if !strings.Contains(ctxText, "*Source: authentication.md*\ndocument_id: doc-auth\n") {
		t.Errorf("kb_search_with_context lost the document_id line:\n%s", ctxText)
	}
}

// TestGetDocumentByPath covers the alternative key: kb_documents.path is
// UNIQUE, so the path printed on a hit identifies the document exactly.
func TestGetDocumentByPath(t *testing.T) {
	cs := connect(t, testServer(t))

	res := callText(t, cs, ToolGetDocument, map[string]any{"path": "/corpus/storage.md"})
	if res.IsError {
		t.Fatalf("kb_get_document by path failed: %s", resultText(t, res))
	}
	text := resultText(t, res)
	for _, want := range []string{"# Storage Layout", "Path: /corpus/storage.md", "FTS5 virtual table"} {
		if !strings.Contains(text, want) {
			t.Errorf("path lookup returned the wrong content, missing %q:\n%s", want, text)
		}
	}

	// Both keys must reach the same document.
	byID := resultText(t, callText(t, cs, ToolGetDocument, map[string]any{"document_id": "doc-storage"}))
	if text != byID {
		t.Errorf("path and document_id lookups disagree:\nby path:\n%s\nby id:\n%s", text, byID)
	}
}

// TestGetDocumentRequiresExactlyOneKey pins the neither/both errors. Both are
// tool errors the model can read and correct, not protocol errors.
func TestGetDocumentRequiresExactlyOneKey(t *testing.T) {
	cs := connect(t, testServer(t))

	cases := map[string]map[string]any{
		"neither":       {},
		"both":          {"document_id": "doc-auth", "path": "/corpus/authentication.md"},
		"blank strings": {"document_id": "", "path": "   "},
	}

	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: ToolGetDocument, Arguments: args})
			if err != nil {
				t.Fatalf("expected an isError result, got protocol error: %v", err)
			}
			if !res.IsError {
				t.Fatalf("expected IsError, got %s", resultText(t, res))
			}
			text := resultText(t, res)
			if !strings.Contains(text, "exactly one of document_id or path") {
				t.Errorf("error does not state the constraint:\n%s", text)
			}
			if !strings.Contains(text, "kb_search") {
				t.Errorf("error does not point at the flow that produces a document_id:\n%s", text)
			}
		})
	}
}

// TestGetDocumentUnknownPathIsQuiet checks the miss path. Paths are matched
// exactly against what is indexed: no filesystem resolution, and no relative
// path is turned into an absolute one. The answer says only "not found".
func TestGetDocumentUnknownPathIsQuiet(t *testing.T) {
	cs := connect(t, testServer(t))

	for _, path := range []string{
		"corpus/authentication.md",      // relative form of a real document
		"/corpus/../corpus/storage.md",  // unnormalised
		"/etc/ssh/ssh_host_ed25519_key", // never indexed
	} {
		t.Run(path, func(t *testing.T) {
			res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
				Name:      ToolGetDocument,
				Arguments: map[string]any{"path": path},
			})
			if err != nil {
				t.Fatalf("expected an isError result, got protocol error: %v", err)
			}
			if !res.IsError {
				t.Fatalf("a non-indexed path returned content: %s", resultText(t, res))
			}
			text := resultText(t, res)
			if !strings.Contains(text, "No indexed document has that path") {
				t.Errorf("unexpected miss message:\n%s", text)
			}
			if !strings.Contains(text, "kb_search") {
				t.Errorf("miss message does not suggest a recovery:\n%s", text)
			}
			// The miss must not leak: not the corpus, and not the key that was
			// tried back into the transcript.
			for _, leak := range []string{"/corpus/authentication.md", "/corpus/storage.md", "doc-auth", path} {
				if strings.Contains(text, leak) {
					t.Errorf("miss message leaked %q:\n%s", leak, text)
				}
			}
		})
	}
}

// TestGetDocumentUnknownIDKeepsItsMessage guards the document_id branch against
// drift while the path branch was added next to it.
func TestGetDocumentUnknownIDKeepsItsMessage(t *testing.T) {
	cs := connect(t, testServer(t))

	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      ToolGetDocument,
		Arguments: map[string]any{"document_id": "no-such-doc"},
	})
	if err != nil {
		t.Fatalf("expected an isError result, got protocol error: %v", err)
	}
	if !res.IsError {
		t.Fatalf("expected IsError, got %s", resultText(t, res))
	}
	if text := resultText(t, res); !strings.Contains(text, "get document: document not found: no-such-doc") {
		t.Errorf("document_id miss message drifted:\n%s", text)
	}
}
