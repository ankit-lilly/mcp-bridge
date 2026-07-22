# mcp-bridge

A Go-based remote MCP bridge that allows any stdio-only MCP host to connect to streamable HTTP MCP servers
over the network, including servers requiring OAuth 2.0 authorization.

## Install

### macOS / Linux

```bash
curl -fsSL https://raw.githubusercontent.com/ankit-lilly/mcp-bridge/main/scripts/install.sh | bash
```
You can read the script before running it by skipping the `|bash` part.

TLDR; it downloads the latest release from GitHub, extracts it, and copies the binary to `/usr/local/bin/mcp-bridge`.
Since it's copying the binary to `/usr/local/bin`, you may need to run it with `sudo` or it may prompt for your password.

Set `INSTALL_DIR` or `VERSION` if you want a different target directory or tag.

Once installed, the command should be available in your path and you can verify it by running:

```bash
mcp-bridge --version
```

Once verified, you can add it to Claude Desktop config directly:

```bash
mcp-bridge configure-claude --name remote-server -- https://your-mcp-url/mcp
```

Manual `claude_desktop_config.json` examples are included below if you prefer to edit the file yourself.

## Quick start

```bash
# Build
make build

# Connect to a remote MCP server (stdio bridge mode)
./bin/mcp-bridge https://remote-mcp-server.example.com/mcp

# Diagnose a remote server
./bin/mcp-bridge inspect https://remote-mcp-server.example.com/mcp

# Add the bridge to Claude Desktop config
./bin/mcp-bridge configure-claude --name remote-server -- https://remote-mcp-server.example.com/mcp
```

## Features

- **Zero Node/npm dependency** — single static Go binary
- **Full-duplex MCP bridging** — stdio ↔ remote transport
- **Remote tool pass-through** — expose the remote server as-is over stdio
- **Streamable HTTP transport** — focused on the current MCP transport
- **Stable local OAuth callback ports** — deterministic defaults per server config
- **OAuth 2.0 Authorization Code + PKCE** with secure local token persistence
- **Claude Desktop config installer** — add or update an `mcpServers` entry safely
- **Cross-platform** — macOS, Linux, Windows

## Usage

### Bridge mode (default)

The bridge runs as a local stdio MCP server and relays traffic to a remote server.
The explicit `bridge` subcommand is also accepted as a compatibility alias:

```bash
mcp-bridge [flags] <server-url> [callback-port]
mcp-bridge bridge [flags] <server-url> [callback-port]
```

If `--callback-port` is omitted, the bridge derives a stable local callback port from the server configuration.

Loopback HTTP URLs such as `http://localhost/...` are allowed without `--allow-http`.
Legacy SSE transport endpoints are not supported.
The old `client` subcommand and `--transport` flag are removed.

### Inspect mode

```bash
mcp-bridge inspect [flags] <server-url>
```

Connects directly and lists server capabilities, tools, resources, and prompts.

### Claude Desktop config installer

```bash
mcp-bridge configure-claude --name <server-name> <server-url> [callback-port]
mcp-bridge configure-claude --name <server-name> [installer-flags] -- [bridge-flags] <server-url> [callback-port]
```

This command updates the **current user's** `claude_desktop_config.json` by merging a new
`mcpServers.<server-name>` entry that points back to the current `mcp-bridge` executable.

#### Installer flags

```
┌──────────────────────────────────┬─────────────────────────────────────────────────────────┐
│               Flag               │                       Description                       │
├──────────────────────────────────┼─────────────────────────────────────────────────────────┤
│ `--name <server-name>`           │  Required Claude Desktop server name                   │
├──────────────────────────────────┼─────────────────────────────────────────────────────────┤
│ `--claude-config <path>`         │  Override the Claude Desktop config file path          │
├──────────────────────────────────┼─────────────────────────────────────────────────────────┤
│ `--dry-run`                      │  Print the merged config JSON without writing          │
├──────────────────────────────────┼─────────────────────────────────────────────────────────┤
│ `--force`                        │  Replace an existing server entry with the same name   │
└──────────────────────────────────┴─────────────────────────────────────────────────────────┘
```

Use `--` as the clean passthrough separator whenever you want to forward bridge flags exactly as provided.
Everything after `--` is parsed as normal bridge arguments, so flags like `--header`, `--resource`,
`--allow-http`, and `--callback-port` belong there.

Examples:

```bash
# Add a simple entry
mcp-bridge configure-claude --name remote-server -- https://remote-mcp-server.example.com/mcp

# Preview the merged config without writing it
mcp-bridge configure-claude --name remote-server --dry-run -- \
  --header 'X-Api-Key:${API_KEY}' \
  https://remote-mcp-server.example.com/mcp

# Replace an existing Claude entry with the same name
mcp-bridge configure-claude --name remote-server --force -- https://remote-mcp-server.example.com/mcp
```

## Configuration

The application stores configuration in `MCP_BRIDGE_CONFIG_DIR` when that environment variable is set.
Otherwise it uses `<os.UserConfigDir()>/mcp-bridge` (for example,
`$HOME/Library/Application Support/mcp-bridge` on macOS).

By default, `configure-claude` writes to
`<os.UserConfigDir()>/Claude/claude_desktop_config.json` for the current user unless `--claude-config`
is provided.

If your server or Kubernetes pod doesn't maintain state and at some point gets restarted or redeployed,
the Claude desktop may try to use the previous `mcpClientId`. When it attempts to redirect you for login,
you may run into "Invalid client" errors.

To fix it, delete that config directory and it should then redirect you to the login screen and create
a new session id from scratch.

### Claude Desktop

Add to your `claude_desktop_config.json`:

```json
{
  "mcpServers": {
    "remote-server": {
      "command": "/path/to/mcp-bridge",
      "args": ["https://remote-mcp-server.example.com/mcp"]
    }
  }
}
```

### With authentication and custom headers

```json
{
  "mcpServers": {
    "remote-server": {
      "command": "/path/to/mcp-bridge",
      "args": [
        "--header", "X-Api-Key:${API_KEY}",
        "https://remote-mcp-server.example.com/mcp"
      ]
    }
  }
}
```

## License

MIT
