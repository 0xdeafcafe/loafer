package slack

import (
	"context"
	"net/url"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/0xdeafcafe/photon/jsonx"
)

// Block Kit's buttons and modals, as the web client works them
// (docs/slack-webapp-methods.md §6). blocks.actions's own fields are
// FOUND; what goes inside actions[], and every views.* argument, are
// guesses from the public interactivity payloads, unchecked against a
// capture.

// Action is one press of an element in a message.
type Action struct {
	ServiceID   string         // the app's bot id (B…), which the web client sends as service_id
	AppID       string         // may be "": not every message says
	TeamID      string         // service_team_id
	Channel, TS string         // the message it's in
	Token       string         // the client_token a view_opened that follows carries
	Payload     map[string]any // action_id, block_id, type, value, selected_option…
}

var tokens atomic.Int64

// ClientToken is a new client_token, the web client's web-<ms> made
// unique within the process.
func ClientToken() string {
	return "web-" + strconv.FormatInt(time.Now().UnixMilli(), 10) + "-" + strconv.FormatInt(tokens.Add(1), 10)
}

// BlockAction tells the app an element was pressed. What it does about
// it (a modal, an edit to the message) comes down the websocket.
func (c *Client) BlockAction(ctx context.Context, a Action) error {
	acts, err := jsonx.Marshal([]map[string]any{a.Payload})
	if err != nil {
		return err
	}
	in, err := jsonx.Marshal(map[string]any{"type": "message", "channel_id": a.Channel, "message_ts": a.TS})
	if err != nil {
		return err
	}
	f := url.Values{"actions": {string(acts)}, "container": {string(in)}, "client_token": {a.Token},
		"service_id": {a.ServiceID}, "service_team_id": {a.TeamID}}
	if a.AppID != "" {
		f.Set("app_id", a.AppID)
	}
	return c.Call(ctx, "blocks.actions", f, nil)
}

// Submitted is what views.submit says back: the app's response_action
// (errors, update, push, clear, or "" for done) and, for errors, what's
// wrong by block_id. UNCERTAIN: that Slack passes these on to the client
// in this shape.
type Submitted struct {
	Action string            `json:"response_action"`
	Errors map[string]string `json:"errors"`
}

// SubmitView sends a modal's inputs, values being Slack's state.values:
// block_id to action_id to the input's value.
func (c *Client) SubmitView(ctx context.Context, viewID, hash, token string, values map[string]map[string]any) (Submitted, error) {
	state, err := jsonx.Marshal(map[string]any{"values": values})
	if err != nil {
		return Submitted{}, err
	}
	f := url.Values{"view_id": {viewID}, "client_token": {token}, "state": {string(state)}}
	if hash != "" {
		f.Set("view_hash", hash)
	}
	var r Submitted
	err = c.Call(ctx, "views.submit", f, &r)
	return r, err
}

// CloseView says a modal was let go without submitting.
func (c *Client) CloseView(ctx context.Context, viewID string) error {
	return c.Call(ctx, "views.close", url.Values{"view_id": {viewID}, "client_token": {ClientToken()}}, nil)
}
