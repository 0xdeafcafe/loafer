// Package slackapp is loafer's own Slack app: a bot that can DM you (which
// Slack pushes to your phone) and a Socket Mode connection for shortcuts
// and /loafer. It never reads your messages; the session token does that.
package slackapp

import (
	"context"
	_ "embed"
	"fmt"
	"net/url"

	"github.com/0xdeafcafe/loafer/internal/slack"
	"github.com/0xdeafcafe/photon/jsonx"
	"github.com/0xdeafcafe/photon/keychain"
)

//go:embed manifest.yaml
var Manifest string

const service = "loafer-app"

// Tokens are the app's, for one workspace.
type Tokens struct {
	TeamID string `json:"team_id"`
	Bot    string `json:"bot"` // xoxb-…
	App    string `json:"app"` // xapp-…, for Socket Mode
	BotID  string `json:"bot_id"`
}

func Save(t Tokens) error {
	b, err := jsonx.Marshal(t)
	if err != nil {
		return err
	}
	return keychain.Write(service, t.TeamID, b)
}

func Load(teamID string) (Tokens, error) {
	b, err := keychain.Read(service, teamID)
	if err != nil {
		return Tokens{}, err
	}
	var t Tokens
	return t, jsonx.Unmarshal(b, &t)
}

// Check verifies both tokens: the bot with auth.test, the app token by
// asking for a Socket Mode URL (which it then doesn't use). It fills in
// TeamID and BotID.
func Check(ctx context.Context, t Tokens) (Tokens, error) {
	who, err := slack.New(slack.Creds{Token: t.Bot}).AuthTest(ctx)
	if err != nil {
		return t, fmt.Errorf("bot token: %w", err)
	}
	t.TeamID, t.BotID = who.TeamID, who.UserID
	if _, err := SocketURL(ctx, t.App); err != nil {
		return t, fmt.Errorf("app token: %w", err)
	}
	return t, nil
}

// SocketURL opens a Socket Mode connection URL with the app token.
func SocketURL(ctx context.Context, app string) (string, error) {
	var r struct {
		URL string `json:"url"`
	}
	err := slack.New(slack.Creds{Token: app}).Call(ctx, "apps.connections.open", nil, &r)
	return r.URL, err
}

// DM sends text to user from the bot. Slack opens the DM itself when
// given a user id, and pushes it to their phone by their own settings.
func DM(ctx context.Context, t Tokens, user, text string) error {
	return slack.New(slack.Creds{Token: t.Bot}).Call(ctx, "chat.postMessage",
		url.Values{"channel": {user}, "text": {text}}, nil)
}
