package setup

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// ServerName is the key Conduit registers itself under in an AI client's MCP
// configuration.
const ServerName = "conduit-kb"

// MCPClient describes where a supported AI client keeps its MCP servers.
type MCPClient struct {
	// ID is the name used on the command line, e.g. "claude-code".
	ID string

	// ConfigPath is the client's configuration file.
	ConfigPath string

	// ServersKey is the top-level key holding the server map. VS Code nests it
	// under "mcp", which is why this is a path rather than a plain key.
	ServersKey string
}

// MCPClients returns every supported client, keyed by ID.
func MCPClients() map[string]MCPClient {
	home, _ := os.UserHomeDir()
	return map[string]MCPClient{
		"claude-code": {
			ID:         "claude-code",
			ConfigPath: filepath.Join(home, ".claude.json"),
			ServersKey: "mcpServers",
		},
		"cursor": {
			ID:         "cursor",
			ConfigPath: filepath.Join(home, ".cursor", "settings", "extensions.json"),
			ServersKey: "mcpServers",
		},
		"vscode": {
			ID:         "vscode",
			ConfigPath: filepath.Join(home, ".vscode", "settings.json"),
			ServersKey: "mcp.servers",
		},
	}
}

// MCPClientIDs returns the supported client IDs in a stable order.
func MCPClientIDs() []string {
	clients := MCPClients()
	ids := make([]string, 0, len(clients))
	for id := range clients {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// LookupMCPClient returns the client with the given ID.
func LookupMCPClient(id string) (MCPClient, error) {
	client, ok := MCPClients()[id]
	if !ok {
		return MCPClient{}, fmt.Errorf("unsupported client: %s", id)
	}
	return client, nil
}

// ConduitCommand returns the command an AI client should launch to reach
// Conduit's MCP server.
//
// It is the ABSOLUTE path of the running executable, not the bare name
// "conduit", and the difference is not cosmetic.
//
// An AI client started from a GUI -- Claude Code from Spotlight, Cursor from
// the Dock -- inherits launchd's or the desktop session's environment, not the
// one a terminal builds. The PATH block install.sh appends to ~/.zshrc is read
// by interactive shells and by nothing else, so a bare "conduit" is looked up
// in a PATH that has never contained ~/.local/bin. The spawn fails with ENOENT
// and the client reports the MCP server as broken, with nothing anywhere
// pointing at PATH as the cause.
//
// With `install.sh --prefix DIR` it is worse: that directory may be on no PATH
// at all, in any process.
//
// Writing the path the binary is actually running from removes the lookup
// entirely. That path is also the right one on principle: it is the copy the
// user just installed and just ran, rather than whichever copy a PATH search
// would happen to find first.
func ConduitCommand() string {
	exe, err := os.Executable()
	if err != nil || exe == "" {
		// A PATH lookup is a worse answer, but it is better than writing an
		// empty command that cannot spawn at all.
		return "conduit"
	}
	if abs, aerr := filepath.Abs(exe); aerr == nil {
		return abs
	}
	return exe
}

// ConfigureResult reports what ConfigureMCPClient did.
type ConfigureResult struct {
	// ClientID is the client that was configured.
	ClientID string

	// ConfigPath is the file that was read (and possibly written).
	ConfigPath string

	// AlreadyConfigured is true when an entry existed and force was not set,
	// in which case nothing was written.
	AlreadyConfigured bool
}

// ConfigureMCPClient registers Conduit's KB MCP server in an AI client's
// configuration.
//
// It merges into whatever is already there rather than replacing the file: a
// client config holds the user's other servers and unrelated settings, and
// losing those would be a far worse failure than not being configured.
func ConfigureMCPClient(clientID string, force bool) (*ConfigureResult, error) {
	client, err := LookupMCPClient(clientID)
	if err != nil {
		return nil, err
	}

	cfg := map[string]interface{}{}
	if data, rerr := os.ReadFile(client.ConfigPath); rerr == nil {
		if uerr := json.Unmarshal(data, &cfg); uerr != nil {
			return nil, fmt.Errorf("parse config: %w", uerr)
		}
	}

	servers, container := serverMap(cfg, client.ServersKey)

	res := &ConfigureResult{ClientID: clientID, ConfigPath: client.ConfigPath}

	if _, exists := servers[ServerName]; exists && !force {
		res.AlreadyConfigured = true
		return res, nil
	}

	servers[ServerName] = map[string]interface{}{
		"command": ConduitCommand(),
		"args":    []string{"mcp", "kb"},
	}
	container[lastSegment(client.ServersKey)] = servers

	if err := os.MkdirAll(filepath.Dir(client.ConfigPath), 0755); err != nil {
		return nil, fmt.Errorf("create config directory: %w", err)
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal config: %w", err)
	}
	if err := writeFileAtomic(client.ConfigPath, data, configFileMode(client.ConfigPath)); err != nil {
		return nil, fmt.Errorf("write config: %w", err)
	}

	return res, nil
}

// RemoveResult reports what RemoveMCPClient did for one client.
type RemoveResult struct {
	// ClientID is the client that was examined.
	ClientID string `json:"clientId"`

	// ConfigPath is the file that was read, and written if Removed is true.
	ConfigPath string `json:"configPath"`

	// Removed is true when a Conduit entry was found and deleted.
	Removed bool `json:"removed"`

	// Missing is true when the client has no config file at all.
	Missing bool `json:"missing"`
}

// RemoveMCPClient deletes Conduit's entry from an AI client's MCP config.
//
// It removes exactly one key and rewrites the rest of the file untouched. An
// uninstall that clobbered a user's other MCP servers, or their unrelated
// editor settings, would do more damage than leaving a stale entry behind --
// so a config that cannot be parsed is reported, never overwritten.
//
// Removing an entry that is not there is not an error: uninstall must be safe
// to run twice.
func RemoveMCPClient(clientID string) (*RemoveResult, error) {
	client, err := LookupMCPClient(clientID)
	if err != nil {
		return nil, err
	}

	res := &RemoveResult{ClientID: clientID, ConfigPath: client.ConfigPath}

	data, err := os.ReadFile(client.ConfigPath)
	if err != nil {
		if os.IsNotExist(err) {
			res.Missing = true
			return res, nil
		}
		return nil, fmt.Errorf("read config: %w", err)
	}

	cfg := map[string]interface{}{}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w (left unchanged)", client.ConfigPath, err)
	}

	servers, container := serverMap(cfg, client.ServersKey)
	if _, exists := servers[ServerName]; !exists {
		return res, nil
	}

	delete(servers, ServerName)
	container[lastSegment(client.ServersKey)] = servers

	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal config: %w", err)
	}
	// Atomic replacement. ~/.claude.json holds every MCP server the user has
	// configured plus Claude Code's own state; a truncating write that dies
	// part way through costs them all of it, and an uninstall is exactly the
	// moment nobody is watching closely.
	if err := writeFileAtomic(client.ConfigPath, out, configFileMode(client.ConfigPath)); err != nil {
		return nil, fmt.Errorf("write config: %w", err)
	}

	res.Removed = true
	return res, nil
}

// configFileMode returns an existing file's permissions, or a sane default.
//
// Rewriting a config must not change who can read it. CreateTemp starts at
// 0600, so without this an atomic replace would quietly tighten -- or, against
// a deliberately group-readable file, alter -- the permissions of a file
// Conduit does not own.
func configFileMode(path string) os.FileMode {
	if info, err := os.Stat(path); err == nil {
		return info.Mode().Perm()
	}
	return 0644
}

// RemoveAllMCPClients strips Conduit from every supported client.
//
// One client failing does not stop the others: a broken Cursor config must not
// leave a stale Claude Code entry pointing at a binary that is about to be
// deleted. Errors are collected and returned alongside the results.
func RemoveAllMCPClients() ([]*RemoveResult, []error) {
	var results []*RemoveResult
	var errs []error

	for _, id := range MCPClientIDs() {
		res, err := RemoveMCPClient(id)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", id, err))
			continue
		}
		results = append(results, res)
	}
	return results, errs
}

// IsMCPClientConfigured reports whether a config file already registers
// Conduit's server, and under what name.
func IsMCPClientConfigured(configPath string) (bool, string) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return false, ""
	}

	var cfg map[string]interface{}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return false, ""
	}

	// Claude Code and Cursor: top-level "mcpServers".
	if servers, ok := cfg["mcpServers"].(map[string]interface{}); ok {
		if _, exists := servers[ServerName]; exists {
			return true, ServerName
		}
	}

	// VS Code: "mcp": {"servers": {...}}.
	if section, ok := cfg["mcp"].(map[string]interface{}); ok {
		if servers, ok := section["servers"].(map[string]interface{}); ok {
			if _, exists := servers[ServerName]; exists {
				return true, ServerName
			}
		}
	}

	return false, ""
}

// serverMap resolves a possibly dotted servers key, creating intermediate
// objects, and returns the server map plus the object that holds it.
func serverMap(cfg map[string]interface{}, key string) (servers, container map[string]interface{}) {
	container = cfg

	// Only the final segment names the server map; anything before it is a
	// nesting level (VS Code's "mcp.servers").
	head, tail := splitKey(key)
	for _, segment := range head {
		next, ok := container[segment].(map[string]interface{})
		if !ok {
			next = map[string]interface{}{}
			container[segment] = next
		}
		container = next
	}

	servers, ok := container[tail].(map[string]interface{})
	if !ok {
		servers = map[string]interface{}{}
	}
	return servers, container
}

// splitKey separates a dotted key into its parent segments and final segment.
func splitKey(key string) (head []string, tail string) {
	var current string
	for _, r := range key {
		if r == '.' {
			head = append(head, current)
			current = ""
			continue
		}
		current += string(r)
	}
	return head, current
}

// lastSegment returns the final segment of a dotted key.
func lastSegment(key string) string {
	_, tail := splitKey(key)
	return tail
}
