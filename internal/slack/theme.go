package slack

import (
	"encoding/json/jsontext"
	"strings"

	"github.com/0xdeafcafe/photon/jsonx"
)

// ThemeColour is the sidebar colour of the theme you gave the workspace,
// from client.userBoot's prefs, as "#rrggbb"; "" when there isn't one, or
// it's Slack's own aubergine, so the workspace gets a colour of its own.
//
// Uncertain: prefs.sidebar_theme_custom_values, a string of JSON with
// column_bg in it, is how the web client has kept a custom theme. Newer
// clients may keep it elsewhere, which only means no colour.
func ThemeColour(prefs jsontext.Value) string {
	var p struct {
		Custom string `json:"sidebar_theme_custom_values"`
	}
	var v struct {
		ColumnBG string `json:"column_bg"`
	}
	if jsonx.Unmarshal(prefs, &p) != nil || jsonx.Unmarshal([]byte(p.Custom), &v) != nil {
		return ""
	}
	c := strings.ToLower(v.ColumnBG)
	if len(c) != 7 || c[0] != '#' || c == "#3f0e40" || c == "#4a154b" {
		return ""
	}
	return c
}
