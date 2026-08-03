package mcpserver

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/simpleflo/conduit/internal/kb"
	"github.com/simpleflo/conduit/internal/observability"
)

// Environment variables used to turn this test binary into an MCP stdio server
// subprocess.
const (
	helperEnv   = "CONDUIT_MCP_STDIO_HELPER"
	helperDBEnv = "CONDUIT_MCP_STDIO_HELPER_DB"

	// helperLogMarker is written to the zerolog logger by the helper process.
	// It must show up on stderr and must NOT show up on stdout.
	helperLogMarker = "STDERR_ONLY_LOG_MARKER"
)

// TestStdioServerHelper is not a test: it is the subprocess entry point used by
// TestStdoutIsProtocolPure. When the guard environment variable is unset it
// does nothing, so it costs nothing during a normal run.
//
// It exits the process directly so the testing framework never gets a chance to
// print "PASS" onto the protocol stream.
func TestStdioServerHelper(t *testing.T) {
	if os.Getenv(helperEnv) != "1" {
		t.Skip("subprocess entry point; not run directly")
	}

	// Real logging, at debug level, so the kb package's per-search debug lines
	// are actually emitted. If any of that leaked to stdout the parent test
	// would catch it.
	observability.SetupDefaultLogging("debug")
	logger := observability.Logger("mcpserver.test")
	logger.Info().Msg(helperLogMarker)

	// The corpus was seeded by the parent process; just open the file.
	db, err := openDB(os.Getenv(helperDBEnv))
	if err != nil {
		logger.Error().Err(err).Msg("open db failed")
		os.Exit(2)
	}
	defer db.Close()

	srv := New(db, kb.NewHybridSearcher(kb.NewSearcher(db), nil))
	if err := srv.Run(context.Background()); err != nil {
		logger.Error().Err(err).Msg("server run failed")
		os.Exit(3)
	}
	os.Exit(0)
}

// TestStdoutIsProtocolPure launches the server as a real stdio subprocess,
// drives it with raw JSON-RPC frames, and proves that stdout carries protocol
// frames and nothing else. Anything else on stdout (a stray fmt.Println, a log
// line, a banner) corrupts the stream for every MCP client.
//
// It doubles as a backwards-compatibility check: the frames below use the
// legacy `initialize` handshake pinned to 2024-11-05, which is what the
// hand-rolled server spoke and what older clients still send.
func TestStdoutIsProtocolPure(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping subprocess test in short mode")
	}

	// Seed the database in the parent so an FTS5-less build skips cleanly here
	// rather than failing inside the subprocess.
	dbPath := filepath.Join(t.TempDir(), "stdio-purity.db")
	db, _, err := openAndSeed(dbPath)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "fts5") {
			t.Skip("FTS5 not available, skipping (build with CGO_ENABLED=1 -tags fts5)")
		}
		t.Fatalf("seed test db: %v", err)
	}
	// Close it: the subprocess opens the same file.
	if err := db.Close(); err != nil {
		t.Fatalf("close seed db: %v", err)
	}

	frames := []string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"raw-stdio-test","version":"1.0.0"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"kb_search","arguments":{"query":"authentication token"}}}`,
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestStdioServerHelper")
	cmd.Env = append(os.Environ(), helperEnv+"=1", helperDBEnv+"="+dbPath)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("stdin pipe: %v", err)
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		t.Fatalf("start stdio server subprocess: %v", err)
	}

	// Keep stdin open while the requests are in flight: a client that hangs up
	// immediately after writing would have its pending calls cancelled, which
	// is a client bug, not a server one.
	for _, frame := range frames {
		if _, err := io.WriteString(stdin, frame+"\n"); err != nil {
			t.Fatalf("write frame: %v\nstderr:\n%s", err, stderr.String())
		}
	}

	scanner := bufio.NewScanner(stdoutPipe)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)

	var stdoutLines []string
	responses := map[float64]map[string]any{}

	// checkLine enforces the purity contract on every byte the server emits.
	checkLine := func(line string) {
		t.Helper()
		stdoutLines = append(stdoutLines, line)
		if strings.TrimSpace(line) == "" {
			return
		}
		var frame map[string]any
		if err := json.Unmarshal([]byte(line), &frame); err != nil {
			t.Fatalf("stdout line is not a JSON-RPC frame (%v): %q\nfull stdout:\n%s\nstderr:\n%s",
				err, line, strings.Join(stdoutLines, "\n"), stderr.String())
		}
		if frame["jsonrpc"] != "2.0" {
			t.Fatalf("stdout line is not a JSON-RPC 2.0 frame: %q", line)
		}
		if id, ok := frame["id"].(float64); ok {
			responses[id] = frame
		}
	}

	for len(responses) < 3 && scanner.Scan() {
		checkLine(scanner.Text())
	}

	// Hang up and drain whatever else the server writes on the way out.
	if err := stdin.Close(); err != nil {
		t.Fatalf("close stdin: %v", err)
	}
	for scanner.Scan() {
		checkLine(scanner.Text())
	}
	if err := scanner.Err(); err != nil && !strings.Contains(err.Error(), "file already closed") {
		t.Fatalf("read stdout: %v", err)
	}

	// A client hanging up is a clean shutdown: the process must exit 0.
	if err := cmd.Wait(); err != nil {
		t.Fatalf("stdio server exited non-zero on client disconnect: %v\nstderr:\n%s", err, stderr.String())
	}

	stdoutAll := strings.Join(stdoutLines, "\n")
	if strings.Contains(stdoutAll, helperLogMarker) {
		t.Errorf("log output leaked onto stdout:\n%s", stdoutAll)
	}
	if !strings.Contains(stderr.String(), helperLogMarker) {
		t.Errorf("expected the log marker on stderr; stderr was:\n%s", stderr.String())
	}
	// The kb package logs a debug line per search. Its presence on stderr is
	// evidence that real log traffic during a tool call also stays off stdout.
	if !strings.Contains(stderr.String(), `"component":"kb.`) {
		t.Errorf("expected kb component logs on stderr; stderr was:\n%s", stderr.String())
	}

	// --- initialize: negotiated down to the client's legacy version --------
	initResp, ok := responses[1]
	if !ok {
		t.Fatalf("no response to initialize; stdout:\n%s", stdoutAll)
	}
	initResult, ok := initResp["result"].(map[string]any)
	if !ok {
		t.Fatalf("initialize returned no result: %+v", initResp)
	}
	if got := initResult["protocolVersion"]; got != "2024-11-05" {
		t.Errorf("negotiated protocol version = %v, want 2024-11-05 (legacy client compatibility)", got)
	}
	serverInfo, _ := initResult["serverInfo"].(map[string]any)
	if serverInfo["name"] != ServerName {
		t.Errorf("serverInfo.name = %v, want %s", serverInfo["name"], ServerName)
	}

	// --- tools/list over the wire ------------------------------------------
	listResp, ok := responses[2]
	if !ok {
		t.Fatalf("no response to tools/list; stdout:\n%s", stdoutAll)
	}
	listResult, _ := listResp["result"].(map[string]any)
	tools, _ := listResult["tools"].([]any)
	if len(tools) != len(ToolNames) {
		t.Errorf("tools/list returned %d tools, want %d", len(tools), len(ToolNames))
	}
	seen := map[string]bool{}
	for _, raw := range tools {
		tool, _ := raw.(map[string]any)
		name, _ := tool["name"].(string)
		seen[name] = true
		if desc, _ := tool["description"].(string); strings.TrimSpace(desc) == "" {
			t.Errorf("tool %q has an empty description on the wire", name)
		}
		if _, ok := tool["inputSchema"].(map[string]any); !ok {
			t.Errorf("tool %q has no inputSchema object on the wire", name)
		}
	}
	for _, want := range ToolNames {
		if !seen[want] {
			t.Errorf("tool %q missing from the wire tools/list", want)
		}
	}

	// --- tools/call over the wire ------------------------------------------
	callResp, ok := responses[3]
	if !ok {
		t.Fatalf("no response to tools/call; stdout:\n%s", stdoutAll)
	}
	callResult, _ := callResp["result"].(map[string]any)
	content, _ := callResult["content"].([]any)
	if len(content) == 0 {
		t.Fatalf("tools/call returned no content: %+v", callResp)
	}
	first, _ := content[0].(map[string]any)
	if first["type"] != "text" {
		t.Errorf("content block type = %v, want text", first["type"])
	}
	if text, _ := first["text"].(string); !strings.Contains(text, "Authentication Design") {
		t.Errorf("tools/call did not return the expected hit: %q", text)
	}
}
