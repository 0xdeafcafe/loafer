package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/0xdeafcafe/loafer/internal/slack"
	"github.com/0xdeafcafe/loafer/internal/slackapp"
)

// appInit sets up loafer's own Slack app: it writes the manifest out,
// takes the bot and app tokens once the app's installed, checks them, and
// has the bot DM you so you can see it reach your phone.
func appInit() error {
	ws, err := slack.Workspaces()
	if err != nil || len(ws) == 0 {
		return fmt.Errorf("run `loafer login` first, so the bot knows who to DM")
	}
	me, err := slack.Load(ws[0].TeamID)
	if err != nil {
		return err
	}
	dir, _ := os.UserConfigDir()
	path := filepath.Join(dir, "loafer", "slack-app-manifest.yaml")
	if err := os.WriteFile(path, []byte(slackapp.Manifest), 0o600); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, `1. Create the app from this manifest (From a manifest → paste it):
     %s
     https://api.slack.com/apps?new_app=1
2. Install it to %s (a workspace admin may need to approve it).
3. Bot token:  OAuth & Permissions → Bot User OAuth Token (xoxb-…)
4. App token:  Basic Information → App-Level Tokens → generate one with
               connections:write (xapp-…)

`, path, ws[0].Team)
	bot, err := ask("bot token: ")
	if err != nil {
		return err
	}
	app, err := ask("app token: ")
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	t, err := slackapp.Check(ctx, slackapp.Tokens{Bot: slack.CleanToken(bot), App: slack.CleanToken(app)})
	if err != nil {
		return err
	}
	if t.TeamID != me.TeamID {
		return fmt.Errorf("that app is installed in another workspace (%s), not %s", t.TeamID, me.TeamID)
	}
	if err := slackapp.Save(t); err != nil {
		return err
	}
	if err := slackapp.DM(ctx, t, me.UserID, "loafer is connected. This is how it'll reach your phone."); err != nil {
		return fmt.Errorf("saved, but the test DM failed: %w", err)
	}
	fmt.Fprintln(os.Stderr, "done: the bot just DMed you")
	return nil
}
