# mcp-bridge

A single-binary MCP bridge that connects any stdio MCP host (Claude Desktop, Claude Code, etc.) to remote streamable HTTP MCP servers — with OAuth 2.0 handled automatically.

## Install

```bash
curl -fsSL https://raw.githubusercontent.com/ankit-lilly/mcp-bridge/main/scripts/install.sh | bash
```

Verify:

```bash
mcp-bridge version
```

## Add to Claude Desktop

```bash
mcp-bridge configure-claude --name my-server -- https://your-mcp-server.example.com/mcp
```

That's it. Restart Claude Desktop and the server will be available.

To pass headers or other options, add them after `--`:

```bash
mcp-bridge configure-claude --name my-server -- \
  --header 'Authorization:Bearer ${TOKEN}' \
  https://your-mcp-server.example.com/mcp
```

## Manual config

If you prefer to edit `claude_desktop_config.json` directly:

```json
{
  "mcpServers": {
    "my-server": {
      "command": "mcp-bridge",
      "args": ["https://your-mcp-server.example.com/mcp"]
    }
  }
}
```

## Troubleshooting

OAuth tokens and client state are stored in `~/.config/mcp-bridge` (or `$MCP_BRIDGE_CONFIG_DIR`). If you hit "Invalid client" errors after a server redeploy, delete that directory and re-authenticate.

Run `mcp-bridge --help` or `mcp-bridge <command> --help` for full flag documentation.

## License

MIT
