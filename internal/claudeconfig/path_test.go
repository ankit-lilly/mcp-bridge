package claudeconfig

import (
	"path/filepath"
	"testing"
)

func TestPathFromUserConfigDir(t *testing.T) {
	base := filepath.Join("/Users", "tester", "Library", "Application Support")
	got := PathFromUserConfigDir(base)
	want := filepath.Join(base, "Claude", "claude_desktop_config.json")
	if got != want {
		t.Fatalf("unexpected path: %s", got)
	}
}
