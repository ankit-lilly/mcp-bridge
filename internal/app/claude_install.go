package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ankit-lilly/mcp-bridge/internal/claudeconfig"
	"github.com/ankit-lilly/mcp-bridge/internal/cli"
)

func RunConfigureClaude(_ context.Context, cfg *cli.ClaudeInstallConfig, ioStreams *IO) error {
	configPath, err := resolveClaudeConfigPath(cfg.ClaudeConfigPath)
	if err != nil {
		return err
	}

	executable, err := currentExecutablePath()
	if err != nil {
		return err
	}

	doc, err := claudeconfig.Load(configPath)
	if err != nil {
		return fmt.Errorf("loading Claude Desktop config: %w", err)
	}

	result, err := doc.SetServer(cfg.Name, claudeconfig.ServerConfig{
		Command: executable,
		Args:    cfg.BridgeArgs,
	}, cfg.Force)
	if err != nil {
		return err
	}

	if cfg.DryRun {
		data, err := doc.MarshalIndent()
		if err != nil {
			return fmt.Errorf("rendering Claude Desktop config: %w", err)
		}
		if _, err := ioStreams.Stdout.Write(data); err != nil {
			return fmt.Errorf("writing dry-run output: %w", err)
		}
		return nil
	}

	if result == claudeconfig.MergeUnchanged {
		fmt.Fprintf(ioStreams.Stderr, "Claude Desktop config already contains %q at %s\n", cfg.Name, configPath)
		return nil
	}

	if err := doc.Write(configPath); err != nil {
		return fmt.Errorf("writing Claude Desktop config: %w", err)
	}

	action := "Added"
	if result == claudeconfig.MergeUpdated {
		action = "Updated"
	}
	fmt.Fprintf(ioStreams.Stderr, "%s Claude Desktop server %q in %s\n", action, cfg.Name, configPath)
	return nil
}

func resolveClaudeConfigPath(override string) (string, error) {
	if override != "" {
		return filepath.Clean(override), nil
	}
	path, err := claudeconfig.DefaultPath()
	if err != nil {
		return "", fmt.Errorf("determining Claude Desktop config path: %w", err)
	}
	return path, nil
}

func currentExecutablePath() (string, error) {
	path, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("determining executable path: %w", err)
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("normalizing executable path: %w", err)
	}
	return absPath, nil
}
