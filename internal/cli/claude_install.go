package cli

import (
	"errors"
	"flag"
	"slices"
	"strings"
)

type ClaudeInstallConfig struct {
	Name             string
	ClaudeConfigPath string
	DryRun           bool
	Force            bool
	BridgeArgs       []string
}

func ParseConfigureClaude(args []string) (*ClaudeInstallConfig, error) {
	installerArgs, bridgeArgs, hasPassthrough := splitPassthroughArgs(args)
	if !hasPassthrough && containsBridgeFlags(installerArgs) {
		return nil, errors.New("bridge flags for configure-claude must be passed after --")
	}

	fs, values := newConfigureClaudeFlagSet()
	if err := fs.Parse(installerArgs); err != nil {
		return nil, err
	}
	if hasPassthrough && len(fs.Args()) > 0 {
		return nil, errors.New("when using configure-claude -- ..., put all bridge arguments after --")
	}

	bridgeArgs = append(fs.Args(), bridgeArgs...)
	bridgeArgs, err := normalizeBridgeArgs(bridgeArgs)
	if err != nil {
		return nil, err
	}
	if _, err := parseBridgeCommand("configure-claude", bridgeUsageLine, bridgeArgs); err != nil {
		return nil, err
	}

	cfg := &ClaudeInstallConfig{
		Name:             values.name,
		ClaudeConfigPath: values.claudeConfigPath,
		DryRun:           values.dryRun,
		Force:            values.force,
		BridgeArgs:       append([]string(nil), bridgeArgs...),
	}
	if strings.TrimSpace(cfg.Name) == "" {
		return nil, errors.New("--name is required")
	}
	if cfg.ClaudeConfigPath != "" && strings.TrimSpace(cfg.ClaudeConfigPath) == "" {
		return nil, errors.New("--claude-config must not be empty")
	}

	return cfg, nil
}

type configureClaudeFlagValues struct {
	name             string
	claudeConfigPath string
	dryRun           bool
	force            bool
}

func newConfigureClaudeFlagSet() (*flag.FlagSet, *configureClaudeFlagValues) {
	values := &configureClaudeFlagValues{}
	fs := flag.NewFlagSet("configure-claude", flag.ContinueOnError)
	setFlagSetUsage(fs, configureClaudeUsageLine, configureClaudePassthroughUsageLine)
	fs.StringVar(&values.name, "name", "", "Claude Desktop server name")
	fs.StringVar(&values.claudeConfigPath, "claude-config", "", "Claude Desktop config path")
	fs.BoolVar(&values.dryRun, "dry-run", false, "Print the merged Claude config without writing")
	fs.BoolVar(&values.force, "force", false, "Replace an existing Claude server entry")
	return fs, values
}

func splitPassthroughArgs(args []string) ([]string, []string, bool) {
	for i, arg := range args {
		if arg == "--" {
			return args[:i], args[i+1:], true
		}
	}
	return args, nil, false
}

func normalizeBridgeArgs(args []string) ([]string, error) {
	if len(args) == 0 {
		return nil, errors.New("server URL is required")
	}

	switch args[0] {
	case "bridge":
		args = args[1:]
	case "inspect":
		return nil, errors.New(`"inspect" is not supported for configure-claude; configure the default bridge mode instead`)
	}

	if len(args) == 0 {
		return nil, errors.New("server URL is required")
	}
	return args, nil
}

func containsBridgeFlags(args []string) bool {
	return slices.ContainsFunc(args, isBridgeFlag)
}

func isBridgeFlag(arg string) bool {
	if !strings.HasPrefix(arg, "--") {
		return false
	}

	name := strings.TrimPrefix(arg, "--")
	if idx := strings.IndexByte(name, '='); idx >= 0 {
		name = name[:idx]
	}
	_, ok := bridgeFlagNames[name]
	return ok
}
