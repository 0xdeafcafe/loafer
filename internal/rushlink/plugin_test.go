package rushlink

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstall(t *testing.T) {
	dir := t.TempDir()
	rush := filepath.Join(dir, "rush")
	if err := os.WriteFile(rush, []byte("#!/bin/sh\necho "+dir+"/plugins\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	plugin := filepath.Join(dir, "loafer-rush")
	_ = os.WriteFile(plugin, []byte("bin"), 0o755)
	c := &CLI{Path: rush}
	if c.Installed() {
		t.Fatal("installed before installing")
	}
	if err := c.Install(plugin, "/opt/loafer", "https://acme.slack.com/"); err != nil {
		t.Fatal(err)
	}
	m, _ := os.ReadFile(filepath.Join(dir, "plugins", "loafer", "plugin.json"))
	if !c.Installed() || !strings.Contains(string(m), `"acme.slack.com:443"`) || !strings.Contains(string(m), `"/opt/loafer"`) || strings.Contains(string(m), "example.slack.com") {
		t.Fatalf("manifest:\n%s", m)
	}
	if err := c.Install(plugin, "/opt/loafer", ""); err == nil {
		t.Fatal("installed with no workspace host")
	}
}
