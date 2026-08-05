package scripts

// `conduit mcp kb` speaks JSON-RPC on stdout. Anything else on that stream --
// one log line, one stray Println, one banner -- corrupts it for every MCP
// client, and the client's report is "the server is broken" with no indication
// of why.
//
// internal/mcpserver already has a purity test, but it constructs the server
// in-process and never runs the CLI. This one drives the REAL binary through
// the REAL command, which is the only way to cover the wiring between them:
// the root command's PersistentPreRun now configures the global logger on every
// invocation, and if that logger were ever pointed at stdout instead of stderr
// every MCP session on every machine would break while the in-process test
// carried on passing.
//
// It lives in this package because that is where the real binary gets built
// (buildRealBinary, tests/scripts/release_test.go).

import (
	"bufio"
	"encoding/json"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestMCPKBStdoutIsProtocolPureUnderDebugLogging(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the real binary; skipped under -short")
	}

	binary := buildRealBinary(t)
	e := newEnv(t)

	// --log-level debug is the point of the test: it turns on the noisiest
	// setting the binary has, on the stream that must stay clean. At any
	// quieter level a stdout-bound logger might emit nothing and the test would
	// prove very little.
	dbPath := filepath.Join(e.home, "mcp-purity", "conduit.db")
	cmd := exec.Command(binary,
		"--log-level", "debug",
		"--db", dbPath,
		"mcp", "kb",
	)
	cmd.Env = []string{
		"HOME=" + e.home,
		"PATH=/usr/bin:/bin",
		"TERM=dumb",
		// Lexical-only: no model, no sidecar, no network.
		"CONDUIT_EMBED_PROVIDER=none",
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("stdin pipe: %v", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		t.Fatalf("start `conduit mcp kb`: %v", err)
	}

	// A real handshake, then two calls that do actual work -- opening the
	// knowledge base and running a search are what produce the debug lines this
	// test is looking for in the wrong place.
	frames := []string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"stdout-purity","version":"1.0.0"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"kb_search","arguments":{"query":"anything at all"}}}`,
	}
	for _, frame := range frames {
		if _, werr := io.WriteString(stdin, frame+"\n"); werr != nil {
			t.Fatalf("write frame: %v\nstderr:\n%s", werr, stderr.String())
		}
	}

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)

	var lines []string
	responses := map[float64]bool{}

	check := func(line string) {
		t.Helper()
		lines = append(lines, line)
		if strings.TrimSpace(line) == "" {
			return
		}

		var frame map[string]any
		if uerr := json.Unmarshal([]byte(line), &frame); uerr != nil {
			t.Fatalf("stdout carried a line that is not JSON: %q\n"+
				"stdout so far:\n%s\nstderr:\n%s", line, strings.Join(lines, "\n"), stderr.String())
		}
		// Valid JSON is not enough: a zerolog line is valid JSON too, and it is
		// exactly what would land here if the logger were pointed at stdout.
		if _, isLog := frame["level"]; isLog {
			t.Fatalf("a LOG LINE reached stdout and would corrupt the protocol stream: %q", line)
		}
		if frame["jsonrpc"] != "2.0" {
			t.Fatalf("stdout line is not a JSON-RPC 2.0 frame: %q", line)
		}
		if id, ok := frame["id"].(float64); ok {
			responses[id] = true
		}
	}

	for len(responses) < 3 && scanner.Scan() {
		check(scanner.Text())
	}
	if err := stdin.Close(); err != nil {
		t.Fatalf("close stdin: %v", err)
	}
	for scanner.Scan() {
		check(scanner.Text())
	}

	if werr := cmd.Wait(); werr != nil {
		t.Fatalf("`conduit mcp kb` exited non-zero on client disconnect: %v\nstderr:\n%s",
			werr, stderr.String())
	}

	if len(responses) < 3 {
		t.Fatalf("only %d of 3 responses arrived; the handshake did not complete\n"+
			"stdout:\n%s\nstderr:\n%s", len(responses), strings.Join(lines, "\n"), stderr.String())
	}

	// The logging did happen -- on the correct stream. Without this the test
	// would also pass if debug logging were simply switched off, which is a
	// different thing from being correctly routed.
	if !strings.Contains(stderr.String(), `"level":"debug"`) {
		t.Errorf("no debug logging appeared on stderr, so this run did not "+
			"exercise the case it exists for:\nstderr:\n%s", stderr.String())
	}
}
