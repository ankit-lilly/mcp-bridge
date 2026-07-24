package config

type ClaudeInstallConfig struct {
	Name             string
	ClaudeConfigPath string
	DryRun           bool
	Force            bool
	BridgeArgs       []string
}
