package claudeconfig

import (
	"fmt"
	"os"
	"path/filepath"
)

const fileName = "claude_desktop_config.json"

// PathFromUserConfigDir returns the Claude Desktop config path under the given config directory.
func PathFromUserConfigDir(base string) string {
	return filepath.Join(base, "Claude", fileName)
}

// DefaultPath returns the current user's Claude Desktop config path.
func DefaultPath() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("determining user config dir: %w", err)
	}
	return PathFromUserConfigDir(base), nil
}
