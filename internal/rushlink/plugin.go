package rushlink

import (
	_ "embed"
	"errors"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/0xdeafcafe/photon/jsonx"
)

// manifest is the rush plugin's (cmd/loafer-rush, docs/rush-plugin.md),
// with a placeholder host and loafer's usual path for Install to fill in.
//
//go:embed plugin.json
var manifest []byte

// PluginDir is where rush keeps the loafer plugin, as rush says.
func (c *CLI) PluginDir() (string, error) {
	out, err := exec.Command(c.Path, "plugin", "dir").Output()
	if err != nil {
		return "", err
	}
	return filepath.Join(strings.TrimSpace(string(out)), "loafer"), nil
}

// Installed says whether the plugin is in rush's plugin folder.
func (c *CLI) Installed() bool {
	dir, err := c.PluginDir()
	if err != nil {
		return false
	}
	_, err = os.Stat(filepath.Join(dir, "plugin.json"))
	return err == nil
}

// Install copies the plugin binary (plugin, loafer-rush) into rush's
// plugin folder with its manifest, made to reach only workspace (its
// url, as signed in) and to run loafer at loafer. rush still has to
// approve it.
func (c *CLI) Install(plugin, loafer, workspace string) error {
	u, err := url.Parse(workspace)
	if err != nil || u.Host == "" {
		return errors.New("no workspace host to let the plugin reach: run loafer login")
	}
	var m map[string]any
	if err := jsonx.Unmarshal(manifest, &m); err != nil {
		return err
	}
	m["network"] = []string{u.Hostname() + ":443"}
	m["exec"].(map[string]any)["loafer"] = []string{loafer, "draft"}
	j, err := jsonx.MarshalIndent(m)
	if err != nil {
		return err
	}
	bin, err := os.ReadFile(plugin)
	if err != nil {
		return err
	}
	dir, err := c.PluginDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "loafer-rush"), bin, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "plugin.json"), append(j, '\n'), 0o644)
}
