package slacktest

import (
	"encoding/json/jsontext"
	"net/url"
	"strings"

	"github.com/0xdeafcafe/photon/jsonx"
)

// Review is the modal deploybot opens whatever of its is pressed, as a
// real app would with views.open: Slack pushes view_opened with the
// press's client_token.
const Review = "V0REVIEW"

var review = jsontext.Value(`{"id":"V0REVIEW","type":"modal","hash":"1.review","app_id":"A0DEPLOY",
 "title":{"type":"plain_text","text":"Review the deploy"},
 "submit":{"type":"plain_text","text":"Send"},"close":{"type":"plain_text","text":"Cancel"},
 "blocks":[
  {"type":"section","block_id":"intro","text":{"type":"mrkdwn","text":"*loafer@4f2a9c1* to production"}},
  {"type":"input","block_id":"verdict","label":{"type":"plain_text","text":"Verdict"},"element":{"type":"radio_buttons","action_id":"verdict_pick",
   "options":[{"text":{"type":"plain_text","text":"ship it"},"value":"ship"},{"text":{"type":"plain_text","text":"hold it"},"value":"hold"}],
   "initial_option":{"text":{"type":"plain_text","text":"ship it"},"value":"ship"}}},
  {"type":"input","block_id":"why","label":{"type":"plain_text","text":"Why"},"element":{"type":"plain_text_input","action_id":"why_text",
   "placeholder":{"type":"plain_text","text":"a line for the log"}}},
  {"type":"input","block_id":"when","optional":true,"label":{"type":"plain_text","text":"When"},"element":{"type":"datepicker","action_id":"when_date"}},
  {"type":"input","block_id":"also","optional":true,"label":{"type":"plain_text","text":"Also to"},"element":{"type":"checkboxes","action_id":"also_pick",
   "options":[{"text":{"type":"plain_text","text":"staging"},"value":"staging"},{"text":{"type":"plain_text","text":"eu"},"value":"eu"}]}},
  {"type":"input","block_id":"team","optional":true,"label":{"type":"plain_text","text":"Team"},"element":{"type":"static_select","action_id":"team_pick",
   "placeholder":{"type":"plain_text","text":"pick a team"},
   "options":[{"text":{"type":"plain_text","text":"bread"},"value":"bread"},{"text":{"type":"plain_text","text":"cake"},"value":"cake"}]}}]}`)

// blocks answers blocks.actions by opening Review, and views.submit by
// taking it unless its why is too short to be one. Call with the lock
// held.
func (s *Server) blocks(method string, f url.Values) (map[string]any, string) {
	if method == "blocks.actions" {
		token := f.Get("client_token")
		if token == "" || f.Get("actions") == "" || f.Get("container") == "" {
			return nil, "invalid_arguments"
		}
		s.pushAny(map[string]any{"type": "view_opened", "view_id": Review, "view_type": "modal",
			"client_token": token, "app_id": "A0DEPLOY", "view": review})
		return map[string]any{}, ""
	}
	if f.Get("view_id") != Review {
		return nil, "not_found"
	}
	var st struct {
		Values map[string]map[string]struct {
			Value *string `json:"value"`
		} `json:"values"`
	}
	if jsonx.Unmarshal([]byte(f.Get("state")), &st) != nil {
		return nil, "invalid_arguments"
	}
	if why := st.Values["why"]["why_text"].Value; why == nil || len(strings.TrimSpace(*why)) < 4 {
		return map[string]any{"response_action": "errors", "errors": map[string]string{"why": "say a little more than that"}}, ""
	}
	return map[string]any{}, ""
}
