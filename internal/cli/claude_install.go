package cli

import (
	"errors"
	"strings"

	"github.com/ankit-lilly/mcp-bridge/internal/config"
	"github.com/spf13/pflag"
)

type ConfigureClaudeOptions struct {
	Name             string
	ClaudeConfigPath string
	DryRun           bool
	Force            bool
}

func NewConfigureClaudeOptions() *ConfigureClaudeOptions {
	return &ConfigureClaudeOptions{}
}

func (o *ConfigureClaudeOptions) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&o.Name, "name", "", "Claude Desktop server name")
	fs.StringVar(&o.ClaudeConfigPath, "claude-config", "", "Claude Desktop config path")
	fs.BoolVar(&o.DryRun, "dry-run", false, "Print the merged Claude config without writing")
	fs.BoolVar(&o.Force, "force", false, "Replace an existing Claude server entry")
}

func (o *ConfigureClaudeOptions) BuildConfig(bridgeArgs []string) (*config.ClaudeInstallConfig, error) {
	if strings.TrimSpace(o.Name) == "" {
		return nil, errors.New("--name is required")
	}
	if o.ClaudeConfigPath != "" && strings.TrimSpace(o.ClaudeConfigPath) == "" {
		return nil, errors.New("--claude-config must not be empty")
	}
	if len(bridgeArgs) == 0 {
		return nil, errors.New("bridge arguments are required after --")
	}
	if bridgeArgs[0] == "bridge" || bridgeArgs[0] == "inspect" {
		return nil, errors.New(`pass only bridge arguments after --, not the "bridge" or "inspect" subcommand`)
	}
	if _, err := parseBridgeArgs(bridgeArgs); err != nil {
		return nil, err
	}

	return &config.ClaudeInstallConfig{
		Name:             o.Name,
		ClaudeConfigPath: o.ClaudeConfigPath,
		DryRun:           o.DryRun,
		Force:            o.Force,
		BridgeArgs:       append([]string(nil), bridgeArgs...),
	}, nil
}
