package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// removedCommand is a command whose backend no longer exists.
//
// These are registered hidden. The alternative -- deleting them outright --
// gives the user cobra's "unknown command" and no idea why a documented
// command vanished. A stub that names the reason and the replacement costs a
// few lines and answers the question. They are hidden so `--help` shows only
// what Conduit can actually do.
type removedCommand struct {
	use   string
	short string
	// why explains what was removed and what to do instead.
	why string
}

// removedCommands is the full v1 surface that WP-3.2 retired.
//
// Two things were deleted and took their commands with them:
//
//   - The daemon. Conduit is one binary that does its work in-process; there
//     is nothing to start, stop, install as a service, or stream events from.
//   - The container runtime, and with it the whole third-party connector
//     subsystem (instances, bindings, container images). Note that the
//     instance lifecycle never actually ran containers: the daemon handlers
//     that claimed to start and stop them only wrote a status column.
var removedCommands = []removedCommand{
	{
		use:   "start",
		short: "(removed) start a connector instance",
		why: "Conduit no longer runs a daemon or containers, so there is nothing to start.\n" +
			"Knowledge base commands work directly: try 'conduit kb sync' or 'conduit status'.",
	},
	{
		use:   "stop",
		short: "(removed) stop a connector instance",
		why: "Conduit no longer runs a daemon or containers, so there is nothing to stop.\n" +
			"Try 'conduit status'.",
	},
	{
		use:   "restart",
		short: "(removed) restart the daemon",
		why: "Conduit no longer runs a daemon. Commands do their work in-process and exit.\n" +
			"Try 'conduit status' or 'conduit doctor'.",
	},
	{
		use:   "service",
		short: "(removed) manage the Conduit background service",
		why: "There is no background service to install, start, stop or remove.\n" +
			"Point your AI client at 'conduit mcp kb' instead: 'conduit mcp configure'.",
	},
	{
		use:   "events",
		short: "(removed) stream daemon events",
		why: "Event streaming came from the daemon's SSE endpoint, which no longer exists.\n" +
			"Commands report their own progress on stdout.",
	},
	{
		use:   "install",
		short: "(removed) install an MCP connector from a repository",
		why: "Third-party connectors ran as containers. The container runtime was removed,\n" +
			"so Conduit cannot install or run them. Configure such servers directly in your\n" +
			"AI client; Conduit provides its own server via 'conduit mcp kb'.",
	},
	{
		use:   "list",
		short: "(removed) list connector instances",
		why:   "Connector instances no longer exist. For knowledge base sources use 'conduit kb list'.",
	},
	{
		use:   "remove",
		short: "(removed) remove a connector instance",
		why: "Connector instances no longer exist. To remove a knowledge base source use\n" +
			"'conduit kb remove <name-or-id>'.",
	},
	{
		use:   "create",
		short: "(removed) create a connector instance",
		why:   "Connector instances no longer exist.",
	},
	{
		use:   "stats",
		short: "(removed) show daemon and instance statistics",
		why:   "Use 'conduit kb stats' for knowledge base statistics, or 'conduit status'.",
	},
	{
		use:   "permissions",
		short: "(removed) show connector instance permissions",
		why:   "Connector instances no longer exist, so there are no per-instance permissions.",
	},
	{
		use:   "audit",
		short: "(removed) audit a connector instance",
		why:   "Connector instances no longer exist.",
	},
	{
		use:   "logs",
		short: "(removed) show connector instance logs",
		why:   "Connector instances no longer exist. For MCP server logs use 'conduit mcp logs'.",
	},
	{
		use:   "client",
		short: "(removed) bind AI clients to connector instances",
		why: "Bindings tied AI clients to connector instances, which no longer exist.\n" +
			"To wire an AI client to Conduit's knowledge base use 'conduit mcp configure'.",
	},
	{
		use:   "deps",
		short: "(removed) manage external dependencies",
		why: "Conduit no longer installs or manages Podman, Docker, Qdrant or FalkorDB;\n" +
			"none of them are used. Run 'conduit doctor' to check what Conduit actually needs.",
	},
	{
		use:   "install-deps",
		short: "(removed) install external dependencies",
		why: "Conduit no longer installs or manages Podman, Docker, Qdrant or FalkorDB;\n" +
			"none of them are used. Run 'conduit doctor' to check what Conduit actually needs.",
	},
	{
		use:   "qdrant",
		short: "(removed) manage the Qdrant vector database",
		why:   "Vectors live in the knowledge base SQLite file since 2.0. There is no Qdrant.",
	},
	{
		use:   "falkordb",
		short: "(removed) manage the FalkorDB graph database",
		why: "The knowledge graph lives in the knowledge base SQLite file since 2.0.\n" +
			"There is no FalkorDB. Enable it with kb.kag.enabled in the config.",
	},
}

// addRemovedCommands registers the retired surface.
func addRemovedCommands(root *cobra.Command) {
	for _, rc := range removedCommands {
		rc := rc
		root.AddCommand(&cobra.Command{
			Use:                rc.use,
			Short:              rc.short,
			Hidden:             true,
			DisableFlagParsing: true,
			RunE: func(cmd *cobra.Command, args []string) error {
				return fmt.Errorf("'conduit %s' was removed in Conduit 2.0.\n\n%s", rc.use, rc.why)
			},
		})
	}
}
