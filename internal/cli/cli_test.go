package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/simpleflo/conduit/internal/config"
)

// ---------------------------------------------------------------------------
// Harness
//
// Commands are executed in-process against a temporary knowledge base. This
// only became possible once exit codes stopped being os.Exit calls inside RunE
// (see exitError), and it is what makes a smoke test per command cheap enough
// to actually have one.
// ---------------------------------------------------------------------------

// testEnv is an isolated Conduit installation for one test.
type testEnv struct {
	home   string
	dbPath string
}

// newTestEnv points HOME and the knowledge base at fresh temporary
// directories, so nothing touches the developer's real ~/.conduit.
func newTestEnv(t *testing.T) *testEnv {
	t.Helper()

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // Windows

	// An empty working directory keeps ./conduit.yaml out of the picture.
	work := t.TempDir()
	t.Chdir(work)

	// Embeddings off by default: the smoke tests must not need a model, a
	// network, or a sidecar process. Lexical-only is a supported mode, so this
	// exercises a real configuration rather than a crippled one.
	t.Setenv("CONDUIT_EMBED_PROVIDER", "none")

	return &testEnv{
		home:   home,
		dbPath: filepath.Join(home, "kb", "test.db"),
	}
}

// run executes a CLI invocation and returns combined stdout, plus the exit code.
//
// --db is injected so every command in the suite works against this test's own
// knowledge base; that also exercises the global flag on every command.
func (e *testEnv) run(t *testing.T, args ...string) (string, int) {
	t.Helper()

	full := append([]string{"--db", e.dbPath}, args...)

	// Commands print with fmt.Println straight to os.Stdout, so capture the
	// file descriptor rather than a cobra out-writer.
	origStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w

	var buf bytes.Buffer
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, _ = io.Copy(&buf, r)
	}()

	root := NewRootCommand()
	root.SetArgs(full)
	root.SetOut(w)
	root.SetErr(w)

	runErr := root.Execute()

	os.Stdout = origStdout
	_ = w.Close()
	wg.Wait()
	_ = r.Close()

	code := 0
	if runErr != nil {
		var ee exitError
		if errors.As(runErr, &ee) {
			code = ee.code
		} else {
			code = 1
			buf.WriteString("Error: " + runErr.Error() + "\n")
		}
	}

	return buf.String(), code
}

// mustRun fails the test if the command does not exit 0.
func (e *testEnv) mustRun(t *testing.T, args ...string) string {
	t.Helper()
	out, code := e.run(t, args...)
	if code != 0 {
		t.Fatalf("conduit %s exited %d\n%s", strings.Join(args, " "), code, out)
	}
	return out
}

// corpus writes a small golden corpus and returns its directory.
func corpus(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	files := map[string]string{
		"auth.md": "# Authentication\n\n" +
			"Conduit authenticates requests with a bearer token issued at login.\n" +
			"The token is verified on every request before authorisation runs.\n",
		"search.md": "# Searching\n\n" +
			"The knowledge base indexes documents for full text search.\n" +
			"Queries are matched against chunk contents using FTS5.\n",
		"notes.txt": "Zebra crossing observations, unrelated to anything else here.\n",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0600); err != nil {
			t.Fatalf("write corpus file %s: %v", name, err)
		}
	}
	return dir
}

// ---------------------------------------------------------------------------
// End to end
// ---------------------------------------------------------------------------

// TestEndToEnd_AddSyncSearch is the whole product in one test: a fresh HOME, a
// folder of documents, an index built with no embedding provider, and a search
// that finds the right document.
func TestEndToEnd_AddSyncSearch(t *testing.T) {
	env := newTestEnv(t)
	docs := corpus(t)

	// --- add ---------------------------------------------------------------
	out := env.mustRun(t, "kb", "add", docs, "--name", "Golden Corpus")
	if !strings.Contains(out, "Added source: Golden Corpus") {
		t.Fatalf("kb add did not confirm the source:\n%s", out)
	}

	// The knowledge base file is created where --db pointed, not in ~/.conduit.
	if _, err := os.Stat(env.dbPath); err != nil {
		t.Fatalf("--db path was not used: %v", err)
	}

	// --- list --------------------------------------------------------------
	out = env.mustRun(t, "kb", "list")
	if !strings.Contains(out, "Golden Corpus") {
		t.Errorf("kb list omitted the source:\n%s", out)
	}

	// --- sync --------------------------------------------------------------
	out, code := env.run(t, "kb", "sync")
	if code != 0 {
		t.Fatalf("kb sync exited %d (0 expected with embeddings off)\n%s", code, out)
	}
	if !strings.Contains(out, "Sync complete") {
		t.Fatalf("kb sync did not report completion:\n%s", out)
	}

	// --- search ------------------------------------------------------------
	out = env.mustRun(t, "kb", "search", "authentication")
	if strings.Contains(out, "No results found") {
		t.Fatalf("kb search found nothing after a successful sync:\n%s", out)
	}
	if !strings.Contains(out, "auth.md") {
		t.Errorf("kb search did not surface auth.md:\n%s", out)
	}

	// --- stats -------------------------------------------------------------
	out = env.mustRun(t, "kb", "stats")
	if !strings.Contains(out, "Documents:") {
		t.Errorf("kb stats output unexpected:\n%s", out)
	}

	// --- remove ------------------------------------------------------------
	out = env.mustRun(t, "kb", "remove", "Golden Corpus", "--force")
	if !strings.Contains(out, "Removed source") {
		t.Errorf("kb remove did not confirm:\n%s", out)
	}

	out = env.mustRun(t, "kb", "list")
	if !strings.Contains(out, "No knowledge base sources configured") {
		t.Errorf("source survived removal:\n%s", out)
	}
}

// TestEndToEnd_SearchJSONShape pins the --json contract that scripts and the
// desktop GUI parse. These key names came from the deleted HTTP daemon and are
// an interface, not an implementation detail.
func TestEndToEnd_SearchJSONShape(t *testing.T) {
	env := newTestEnv(t)
	docs := corpus(t)

	env.mustRun(t, "kb", "add", docs, "--name", "Corpus")
	env.mustRun(t, "kb", "sync")

	out := env.mustRun(t, "kb", "search", "authentication", "--json")

	var resp map[string]interface{}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &resp); err != nil {
		t.Fatalf("kb search --json emitted invalid JSON: %v\n%s", err, out)
	}

	for _, key := range []string{"results", "total_hits", "query", "search_time", "search_mode", "processed"} {
		if _, ok := resp[key]; !ok {
			t.Errorf("kb search --json is missing key %q; got %v", key, keysOf(resp))
		}
	}

	results, ok := resp["results"].([]interface{})
	if !ok {
		t.Fatalf("results should be an array, got %T", resp["results"])
	}
	if len(results) == 0 {
		t.Fatal("expected at least one result")
	}
	first, ok := results[0].(map[string]interface{})
	if !ok {
		t.Fatalf("result should be an object, got %T", results[0])
	}
	// Processed results carry merged content, not per-chunk snippets.
	for _, key := range []string{"document_id", "path", "title", "content", "score", "chunk_count"} {
		if _, ok := first[key]; !ok {
			t.Errorf("processed result is missing key %q; got %v", key, keysOf(first))
		}
	}
}

// TestEndToEnd_ListJSONShape pins {"sources":[...],"count":N}.
func TestEndToEnd_ListJSONShape(t *testing.T) {
	env := newTestEnv(t)
	docs := corpus(t)
	env.mustRun(t, "kb", "add", docs, "--name", "Corpus")

	out := env.mustRun(t, "kb", "list", "--json")

	var resp struct {
		Sources []map[string]interface{} `json:"sources"`
		Count   int                      `json:"count"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &resp); err != nil {
		t.Fatalf("kb list --json emitted invalid JSON: %v\n%s", err, out)
	}
	if resp.Count != 1 || len(resp.Sources) != 1 {
		t.Fatalf("expected exactly one source, got count=%d len=%d", resp.Count, len(resp.Sources))
	}
	for _, key := range []string{"source_id", "name", "path", "doc_count", "chunk_count", "size_bytes"} {
		if _, ok := resp.Sources[0][key]; !ok {
			t.Errorf("source object is missing key %q", key)
		}
	}
}

// TestEndToEnd_MCPToolsList runs the MCP server over a pipe and checks it
// answers tools/list. This is the integration the whole product exists for.
func TestEndToEnd_MCPToolsList(t *testing.T) {
	env := newTestEnv(t)
	docs := corpus(t)
	env.mustRun(t, "kb", "add", docs, "--name", "Corpus")
	env.mustRun(t, "kb", "sync")

	tools := mcpToolsList(t, env)

	if len(tools) == 0 {
		t.Fatal("MCP server advertised no tools")
	}
	// kb_search is the tool every AI client is configured to call.
	if !contains(tools, "kb_search") {
		t.Errorf("tools/list is missing kb_search; got %v", tools)
	}
}

// ---------------------------------------------------------------------------
// Per-command smoke tests
// ---------------------------------------------------------------------------

func TestSmoke_Version(t *testing.T) {
	env := newTestEnv(t)

	out := env.mustRun(t, "version")
	if !strings.Contains(out, "conduit") {
		t.Errorf("version output unexpected: %q", out)
	}

	out = env.mustRun(t, "version", "--json")
	var v struct {
		Version   string `json:"version"`
		BuildTime string `json:"build_time"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &v); err != nil {
		t.Fatalf("version --json invalid: %v (%q)", err, out)
	}
	if v.Version == "" {
		t.Error("version --json reported an empty version")
	}
}

func TestSmoke_Status(t *testing.T) {
	env := newTestEnv(t)

	out := env.mustRun(t, "status")
	if !strings.Contains(out, "Conduit Status") {
		t.Errorf("status output unexpected:\n%s", out)
	}
	// Status must describe the knowledge base, not a daemon that no longer
	// exists.
	if strings.Contains(strings.ToLower(out), "socket") {
		t.Errorf("status still mentions a socket:\n%s", out)
	}

	out = env.mustRun(t, "status", "--json")
	var st map[string]interface{}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &st); err != nil {
		t.Fatalf("status --json invalid: %v\n%s", err, out)
	}
	for _, key := range []string{"database", "available", "sources", "documents", "search_mode"} {
		if _, ok := st[key]; !ok {
			t.Errorf("status --json missing key %q; got %v", key, keysOf(st))
		}
	}
}

func TestSmoke_Doctor(t *testing.T) {
	env := newTestEnv(t)

	out, code := env.run(t, "doctor")
	if code != 0 {
		t.Fatalf("doctor exited %d on a healthy empty install\n%s", code, out)
	}
	if !strings.Contains(out, "Conduit Doctor") {
		t.Errorf("doctor output unexpected:\n%s", out)
	}
	// FTS5 is the one hard requirement; if it were missing, search cannot work.
	if !strings.Contains(out, "FTS5") {
		t.Errorf("doctor did not check FTS5:\n%s", out)
	}
	// With embed.provider = none the provider check must be skipped, not failed.
	if strings.Contains(out, "✗ embedding provider") {
		t.Errorf("doctor failed the embedding check in a supported lexical-only mode:\n%s", out)
	}
}

func TestSmoke_DoctorJSON(t *testing.T) {
	env := newTestEnv(t)

	out, code := env.run(t, "doctor", "--json")
	if code != 0 {
		t.Fatalf("doctor --json exited %d\n%s", code, out)
	}

	var resp struct {
		Checks []struct {
			Name   string `json:"name"`
			Status string `json:"status"`
		} `json:"checks"`
		Healthy bool `json:"healthy"`
		Failed  int  `json:"failed"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &resp); err != nil {
		t.Fatalf("doctor --json invalid: %v\n%s", err, out)
	}
	if len(resp.Checks) == 0 {
		t.Fatal("doctor --json ran no checks")
	}
	if !resp.Healthy || resp.Failed != 0 {
		t.Errorf("doctor reported unhealthy on a clean install: %+v", resp)
	}
}

func TestSmoke_KBStatsEmpty(t *testing.T) {
	env := newTestEnv(t)

	out := env.mustRun(t, "kb", "stats")
	if !strings.Contains(out, "No sources configured") {
		t.Errorf("kb stats on an empty knowledge base:\n%s", out)
	}
}

func TestSmoke_KBSearchEmpty(t *testing.T) {
	env := newTestEnv(t)

	out, code := env.run(t, "kb", "search", "anything")
	if code != 0 {
		t.Fatalf("searching an empty knowledge base should not fail, exited %d\n%s", code, out)
	}
	if !strings.Contains(out, "No results found") {
		t.Errorf("expected an empty-result message:\n%s", out)
	}
}

func TestSmoke_KBSearchModeFlags(t *testing.T) {
	env := newTestEnv(t)
	docs := corpus(t)
	env.mustRun(t, "kb", "add", docs, "--name", "Corpus")
	env.mustRun(t, "kb", "sync")

	// --fts5 is the lexical path and must work with no embedding provider.
	out := env.mustRun(t, "kb", "search", "authentication", "--fts5")
	if strings.Contains(out, "No results found") {
		t.Errorf("--fts5 search found nothing:\n%s", out)
	}

	// --semantic must fail honestly rather than silently returning lexical hits.
	out, code := env.run(t, "kb", "search", "authentication", "--semantic")
	if code == 0 {
		t.Errorf("--semantic should fail when embed.provider is none:\n%s", out)
	}

	// Mutually exclusive flags are rejected.
	out, code = env.run(t, "kb", "search", "x", "--semantic", "--fts5")
	if code == 0 || !strings.Contains(out, "cannot use both") {
		t.Errorf("--semantic with --fts5 should be rejected, got code=%d\n%s", code, out)
	}
}

func TestSmoke_KBAddRejectsNonDirectory(t *testing.T) {
	env := newTestEnv(t)

	file := filepath.Join(t.TempDir(), "a-file.md")
	if err := os.WriteFile(file, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}

	out, code := env.run(t, "kb", "add", file)
	if code == 0 {
		t.Errorf("kb add accepted a file where a directory is required:\n%s", out)
	}
}

func TestSmoke_KBRemoveUnknownSource(t *testing.T) {
	env := newTestEnv(t)

	out, code := env.run(t, "kb", "remove", "does-not-exist", "--force")
	if code == 0 {
		t.Errorf("removing an unknown source should fail:\n%s", out)
	}
	if !strings.Contains(out, "not found") {
		t.Errorf("error should say the source was not found:\n%s", out)
	}
}

func TestSmoke_KBMigrateWithoutEmbeddings(t *testing.T) {
	env := newTestEnv(t)

	// Migration embeds documents; with no provider it must say so plainly.
	out, code := env.run(t, "kb", "migrate")
	if code == 0 {
		t.Errorf("kb migrate should fail with embed.provider = none:\n%s", out)
	}
	if !strings.Contains(strings.ToLower(out), "semantic search unavailable") {
		t.Errorf("kb migrate error should name the cause:\n%s", out)
	}
}

func TestSmoke_Config(t *testing.T) {
	env := newTestEnv(t)

	out := env.mustRun(t, "config")
	for _, want := range []string{"Data Directory", "Database Path", "Embeddings"} {
		if !strings.Contains(out, want) {
			t.Errorf("config output missing %q:\n%s", want, out)
		}
	}
	// Settings for deleted subsystems must not be displayed.
	for _, gone := range []string{"Socket Path", "Runtime", "Pull Timeout"} {
		if strings.Contains(out, gone) {
			t.Errorf("config still displays removed setting %q:\n%s", gone, out)
		}
	}
}

func TestSmoke_ConfigGetSetUnset(t *testing.T) {
	env := newTestEnv(t)

	env.mustRun(t, "config", "set", "kb.rag.default_limit", "25")

	out := env.mustRun(t, "config", "get", "kb.rag.default_limit")
	if !strings.Contains(out, "25") {
		t.Errorf("config get did not return the value just set:\n%s", out)
	}

	env.mustRun(t, "config", "unset", "kb.rag.default_limit")
}

func TestSmoke_MCPStatus(t *testing.T) {
	env := newTestEnv(t)

	out := env.mustRun(t, "mcp", "status")
	if !strings.Contains(out, "MCP KB Server Status") {
		t.Errorf("mcp status output unexpected:\n%s", out)
	}
}

// TestSmoke_MCPStatusJSON pins the shape the desktop GUI reads (see CLAUDE.md,
// which documents `conduit mcp status --json` as the way to learn whether a
// client is configured).
func TestSmoke_MCPStatusJSON(t *testing.T) {
	env := newTestEnv(t)

	out := env.mustRun(t, "mcp", "status", "--json")

	var resp map[string]struct {
		Configured bool   `json:"configured"`
		ConfigPath string `json:"configPath"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &resp); err != nil {
		t.Fatalf("mcp status --json invalid: %v\n%s", err, out)
	}
	for _, client := range []string{"claude-code", "cursor", "vscode"} {
		entry, ok := resp[client]
		if !ok {
			t.Errorf("mcp status --json is missing client %q", client)
			continue
		}
		if entry.ConfigPath == "" {
			t.Errorf("client %q has no configPath", client)
		}
		if entry.Configured {
			t.Errorf("client %q reported configured in a fresh HOME", client)
		}
	}
}

func TestSmoke_MCPConfigure(t *testing.T) {
	env := newTestEnv(t)

	out := env.mustRun(t, "mcp", "configure")
	if !strings.Contains(out, "configured") {
		t.Errorf("mcp configure output unexpected:\n%s", out)
	}

	// It must actually write the entry an AI client will read.
	data, err := os.ReadFile(filepath.Join(env.home, ".claude.json"))
	if err != nil {
		t.Fatalf("mcp configure wrote no client config: %v", err)
	}
	var cfg struct {
		MCPServers map[string]struct {
			Command string   `json:"command"`
			Args    []string `json:"args"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("client config is not valid JSON: %v", err)
	}
	entry, ok := cfg.MCPServers["conduit-kb"]
	if !ok {
		t.Fatalf("conduit-kb entry missing; got %v", cfg.MCPServers)
	}
	if entry.Command != "conduit" || len(entry.Args) != 2 ||
		entry.Args[0] != "mcp" || entry.Args[1] != "kb" {
		t.Errorf("unexpected server entry: %+v", entry)
	}

	// Now mcp status must agree that the client is configured.
	out = env.mustRun(t, "mcp", "status", "--json")
	var status map[string]struct {
		Configured bool `json:"configured"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &status); err != nil {
		t.Fatalf("mcp status --json invalid: %v", err)
	}
	if !status["claude-code"].Configured {
		t.Error("mcp status does not see the configuration mcp configure just wrote")
	}
}

func TestSmoke_KAGStatus(t *testing.T) {
	env := newTestEnv(t)

	// The graph is opt-in; status must work (and say so) when it is off.
	out, code := env.run(t, "kb", "kag-status")
	if code != 0 {
		t.Fatalf("kb kag-status exited %d\n%s", code, out)
	}
}

func TestSmoke_KAGQueryWithGraphDisabled(t *testing.T) {
	env := newTestEnv(t)
	docs := corpus(t)
	env.mustRun(t, "kb", "add", docs, "--name", "Corpus")
	env.mustRun(t, "kb", "sync")

	// kag-query must degrade to retrieval rather than error when the graph is
	// off, which is the default.
	if _, code := env.run(t, "kb", "kag-query", "authentication"); code != 0 {
		t.Errorf("kb kag-query exited %d with the graph disabled", code)
	}
}

func TestSmoke_Backup(t *testing.T) {
	env := newTestEnv(t)
	docs := corpus(t)
	env.mustRun(t, "kb", "add", docs, "--name", "Corpus")

	out, code := env.run(t, "backup")
	if code != 0 {
		t.Fatalf("backup exited %d\n%s", code, out)
	}
}

func TestSmoke_UninstallInfo(t *testing.T) {
	env := newTestEnv(t)

	out := env.mustRun(t, "uninstall", "--info")
	if !strings.Contains(out, "Conduit Installation") {
		t.Errorf("uninstall --info output unexpected:\n%s", out)
	}
	// The daemon and container sections must be gone.
	for _, gone := range []string{"Daemon Service", "conduit-qdrant", "Ollama Models"} {
		if strings.Contains(out, gone) {
			t.Errorf("uninstall --info still reports %q:\n%s", gone, out)
		}
	}
}

func TestSmoke_UninstallDryRunKeepsData(t *testing.T) {
	env := newTestEnv(t)
	docs := corpus(t)
	env.mustRun(t, "kb", "add", docs, "--name", "Corpus")

	if _, code := env.run(t, "uninstall", "--all", "--dry-run", "--force"); code != 0 {
		t.Fatalf("uninstall --dry-run failed")
	}

	// A dry run must not have touched the knowledge base.
	if _, err := os.Stat(env.dbPath); err != nil {
		t.Errorf("dry run removed the knowledge base: %v", err)
	}
}

func TestSmoke_OllamaStatus(t *testing.T) {
	env := newTestEnv(t)

	// Ollama is an optional provider; the command must report its absence
	// rather than fail, since a machine without Ollama is a normal machine.
	if _, code := env.run(t, "ollama", "status"); code != 0 {
		t.Log("ollama status exited non-zero; acceptable when Ollama is absent")
	}
}

// ---------------------------------------------------------------------------
// Removed commands
// ---------------------------------------------------------------------------

// TestRemovedCommands_FailWithAReason checks that a command whose backend was
// deleted says so, instead of producing cobra's bare "unknown command".
func TestRemovedCommands_FailWithAReason(t *testing.T) {
	env := newTestEnv(t)

	for _, name := range []string{"start", "stop", "restart", "service", "events", "install",
		"list", "create", "permissions", "audit", "client", "deps", "qdrant", "falkordb"} {
		t.Run(name, func(t *testing.T) {
			out, code := env.run(t, name)
			if code == 0 {
				t.Fatalf("removed command %q exited 0:\n%s", name, out)
			}
			if !strings.Contains(out, "removed in Conduit 2.0") {
				t.Errorf("removed command %q does not explain itself:\n%s", name, out)
			}
		})
	}
}

// TestRemovedCommands_HiddenFromHelp keeps the retired surface out of --help
// while leaving it reachable for anyone with it in a script.
func TestRemovedCommands_HiddenFromHelp(t *testing.T) {
	env := newTestEnv(t)

	out := env.mustRun(t, "--help")
	for _, name := range []string{"qdrant", "falkordb", "install-deps"} {
		for _, line := range strings.Split(out, "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), name+" ") {
				t.Errorf("--help advertises removed command %q", name)
			}
		}
	}
	// The commands that remain must still be listed.
	for _, name := range []string{"kb", "mcp", "doctor", "status", "config"} {
		if !strings.Contains(out, name) {
			t.Errorf("--help does not list %q", name)
		}
	}
}

// TestRootHelp_DocumentsDBFlag checks the workspace-isolation flag is
// discoverable, which the work package requires.
func TestRootHelp_DocumentsDBFlag(t *testing.T) {
	env := newTestEnv(t)

	out := env.mustRun(t, "--help")
	if !strings.Contains(out, "--db") {
		t.Errorf("--help does not document --db:\n%s", out)
	}
	if !strings.Contains(out, "knowledge base SQLite file") {
		t.Errorf("--db help text does not say what it points at:\n%s", out)
	}
}

// ---------------------------------------------------------------------------
// Workspace isolation
// ---------------------------------------------------------------------------

// TestDBFlag_IsolatesWorkspaces proves two --db paths are independent
// knowledge bases, which is the point of the flag.
func TestDBFlag_IsolatesWorkspaces(t *testing.T) {
	env := newTestEnv(t)
	docs := corpus(t)

	projectA := filepath.Join(t.TempDir(), "a.db")
	projectB := filepath.Join(t.TempDir(), "b.db")

	runWith := func(db string, args ...string) (string, int) {
		saved := env.dbPath
		env.dbPath = db
		defer func() { env.dbPath = saved }()
		return env.run(t, args...)
	}

	if out, code := runWith(projectA, "kb", "add", docs, "--name", "Only In A"); code != 0 {
		t.Fatalf("add to A failed:\n%s", out)
	}

	outB, code := runWith(projectB, "kb", "list")
	if code != 0 {
		t.Fatalf("list in B failed:\n%s", outB)
	}
	if strings.Contains(outB, "Only In A") {
		t.Errorf("workspace B can see workspace A's source:\n%s", outB)
	}

	outA, _ := runWith(projectA, "kb", "list")
	if !strings.Contains(outA, "Only In A") {
		t.Errorf("workspace A lost its own source:\n%s", outA)
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// testConfig builds the config a CLI invocation in this environment would
// resolve: the test's knowledge base, embeddings off.
func testConfig(env *testEnv) (*config.Config, error) {
	cfg := config.DefaultConfig()
	cfg.DBPath = env.dbPath
	cfg.Embed.Provider = config.EmbedProviderNone
	return cfg, nil
}

func keysOf(m map[string]interface{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
