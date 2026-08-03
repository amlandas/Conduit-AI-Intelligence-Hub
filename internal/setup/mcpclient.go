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
		"command": "conduit",
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
	if err := os.WriteFile(client.ConfigPath, data, 0644); err != nil {
		return nil, fmt.Errorf("write config: %w", err)
	}

	return res, nil
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
