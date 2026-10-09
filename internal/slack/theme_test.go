package slack

import "testing"

func TestThemeColour(t *testing.T) {
	for prefs, want := range map[string]string{
		`{"sidebar_theme_custom_values":"{\"column_bg\":\"#0B4F6C\",\"menu_bg\":\"#083c52\"}"}`: "#0b4f6c",
		`{"sidebar_theme_custom_values":"{\"column_bg\":\"#3F0E40\"}"}`:                         "", // Slack's own
		`{"sidebar_theme_custom_values":""}`:                                                    "",
		`{"sidebar_theme":"aubergine"}`:                                                         "",
		`{"sidebar_theme_custom_values":"{\"column_bg\":\"red\"}"}`:                             "",
		``: "",
	} {
		if got := ThemeColour([]byte(prefs)); got != want {
			t.Errorf("%s: %q, want %q", prefs, got, want)
		}
	}
}
