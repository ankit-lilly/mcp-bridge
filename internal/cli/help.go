package cli

import (
	"flag"
	"fmt"
	"io"
)

const (
	bridgeUsageLine                     = "mcp-bridge [flags] <server-url> [callback-port]"
	inspectUsageLine                    = "mcp-bridge inspect [flags] <server-url>"
	configureClaudeUsageLine            = "mcp-bridge configure-claude --name <server-name> <server-url> [callback-port]"
	configureClaudePassthroughUsageLine = "mcp-bridge configure-claude --name <server-name> [installer-flags] -- [bridge-flags] <server-url> [callback-port]"
)

// WriteRootHelp writes the top-level CLI help including subcommands.
func WriteRootHelp(w io.Writer) {
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintf(w, "  %s\n", bridgeUsageLine)
	fmt.Fprintf(w, "  %s\n", inspectUsageLine)
	fmt.Fprintf(w, "  %s\n", configureClaudeUsageLine)
	fmt.Fprintf(w, "  %s\n", configureClaudePassthroughUsageLine)
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Commands:")
	fmt.Fprintln(w, "  inspect            Diagnose a remote server")
	fmt.Fprintln(w, "  configure-claude   Add or update a Claude Desktop mcpServers entry")
	fmt.Fprintln(w, "  bridge             Compatibility alias for the default bridge mode")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Run 'mcp-bridge <command> --help' for command-specific flags.")
}

func setFlagSetUsage(fs *flag.FlagSet, lines ...string) {
	fs.Usage = func() {
		out := fs.Output()
		fmt.Fprintln(out, "Usage:")
		for _, line := range lines {
			fmt.Fprintf(out, "  %s\n", line)
		}
		fmt.Fprintln(out)
		fmt.Fprintln(out, "Flags:")
		fs.PrintDefaults()
	}
}
