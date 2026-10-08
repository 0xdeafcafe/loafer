package slack

import (
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/0xdeafcafe/photon/jsonx"
	"github.com/0xdeafcafe/photon/keychain"
)

// Each workspace's creds are one Keychain item (service "loafer", account
// the team id). Which workspaces there are is kept beside the config, with
// nothing secret in it.

const service = "loafer"

// Workspace is a signed-in workspace, as listed (no secrets).
type Workspace struct {
	TeamID string `json:"team_id"`
	Team   string `json:"team"`
	URL    string `json:"url"`
}

func indexPath() string {
	d, _ := os.UserConfigDir()
	return filepath.Join(d, "loafer", "workspaces.json")
}

// Workspaces lists the signed-in workspaces, the first being the default.
func Workspaces() ([]Workspace, error) {
	b, err := os.ReadFile(indexPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var ws []Workspace
	return ws, jsonx.Unmarshal(b, &ws)
}

// Save keeps c in the Keychain and lists its workspace first.
func Save(c Creds) error {
	b, err := jsonx.Marshal(c)
	if err != nil {
		return err
	}
	if err := keychain.Write(service, c.TeamID, b); err != nil {
		return err
	}
	ws, err := Workspaces()
	if err != nil {
		return err
	}
	ws = slices.DeleteFunc(ws, func(w Workspace) bool { return w.TeamID == c.TeamID })
	ws = append([]Workspace{{c.TeamID, c.Team, c.URL}}, ws...)
	if b, err = jsonx.MarshalIndent(ws); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(indexPath()), 0o700); err != nil {
		return err
	}
	return os.WriteFile(indexPath(), b, 0o600)
}

// Load reads a workspace's creds from the Keychain.
func Load(teamID string) (Creds, error) {
	b, err := keychain.Read(service, teamID)
	if err != nil {
		return Creds{}, err
	}
	var c Creds
	return c, jsonx.Unmarshal(b, &c)
}

// CleanToken and CleanCookie take what was pasted from DevTools, which
// may carry quotes, "d=" or a trailing ";", and give the value to send.
func CleanToken(s string) string { return strings.Trim(strings.TrimSpace(s), `"'`) }

func CleanCookie(s string) string {
	s = strings.Trim(strings.TrimSpace(s), `"'`)
	s = strings.TrimPrefix(s, "d=")
	s, _, _ = strings.Cut(s, ";")
	// DevTools can show the cookie decoded; Slack wants it as stored.
	if strings.ContainsAny(s, "/+=") {
		s = url.QueryEscape(s)
	}
	return s
}
