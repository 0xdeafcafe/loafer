package slacktest

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/0xdeafcafe/loafer/internal/slack"
)

// The workspace: a small bakery-adjacent software team. Times are set
// back from when the server starts, so it always reads as today's.

// People.
const (
	Self      = "U0SAM" // you
	Priya     = "U0PRIYA"
	Jo        = "U0JO"
	Tomas     = "U0TOMAS"
	DeployBot = "U0DEPLOY"
)

// Conversations.
const (
	General  = "C0GENERAL"
	Dev      = "C0DEV"
	Random   = "C0RANDOM"
	Alerts   = "C0ALERTS"
	Design   = "C0DESIGN" // private
	Pub      = "G0PUB"    // a group DM: you, priya and jo
	PriyaDM  = "D0PRIYA"
	JoDM     = "D0JO"
	DeployDM = "D0DEPLOY"
	Books    = "C0BOOKS" // a channel you're not in
	Old      = "C0OLD"   // an archived one
)

func (s *Server) seed(now time.Time) {
	s.team = slack.Team{ID: "T0CRUMB", Name: "Crumb & Co", Domain: "crumb"}
	person := func(id, handle, real, color string) slack.User {
		u := slack.User{ID: id, Name: handle, RealName: real, Color: color, TZ: "Europe/London"}
		u.Profile.DisplayName, u.Profile.RealName = handle, real
		return u
	}
	bot := person(DeployBot, "deploybot", "deploybot", "2eb67d")
	bot.IsBot, bot.Profile.BotID = true, "B0DEPLOY"
	s.users = []slack.User{
		person(Self, "sam", "Sam Baker", "9f69e7"),
		person(Priya, "priya", "Priya Shah", "e7392d"),
		person(Jo, "jo", "Jo Okafor", "3c989f"),
		person(Tomas, "tomás", "Tomás Reyes", "e0a729"),
		bot,
	}
	s.emoji = map[string]string{"shipit": "alias:rocket"}

	channel := func(id, name, topic string, members int) slack.Conversation {
		return slack.Conversation{ID: id, Name: name, IsChannel: true, IsMember: true, Topic: slack.Text{Value: topic}, NumMembers: members}
	}
	im := func(id, user string) slack.Conversation {
		return slack.Conversation{ID: id, IsIM: true, IsOpen: true, User: user}
	}
	design := channel(Design, "design", "", 3)
	design.IsPrivate = true
	for _, c := range []slack.Conversation{
		channel(General, "general", "everyone, and the lunch plans", 5),
		channel(Dev, "dev", "loafer, mostly. reviews in threads please", 4),
		channel(Random, "random", "", 5),
		channel(Alerts, "alerts", "deploybot shouts here", 4),
		design,
		{ID: Pub, Name: "mpdm-sam--priya--jo-1", IsMPIM: true, IsGroup: true, IsPrivate: true, IsMember: true, IsOpen: true},
		im(PriyaDM, Priya), im(JoDM, Jo), im(DeployDM, DeployBot),
	} {
		s.convs[c.ID] = &conv{Conversation: c, replies: map[string][]slack.Message{}}
		s.order = append(s.order, c.ID)
	}

	section := func(id, name, typ, emoji, next string, ids ...string) slack.Section {
		x := slack.Section{ID: id, Name: name, Type: typ, Emoji: emoji, Next: next}
		x.Channels.IDs = ids
		return x
	}
	s.sections = []slack.Section{
		section("S0STARS", "Starred", "stars", "", "S0TEAM", Dev),
		section("S0TEAM", "Team", "standard", ":bread:", "S0CHANNELS", General, Design),
		section("S0CHANNELS", "Channels", "channels", "", "S0DMS"),
		section("S0DMS", "Direct messages", "direct_messages", "", "S0APPS"),
		section("S0APPS", "Apps", "recent_apps", "", ""),
	}

	say := func(user, text string) slack.Message { return slack.Message{Type: "message", User: user, Text: text} }
	alert := func(text, color, title, body string) slack.Message {
		return slack.Message{Type: "message", Subtype: "bot_message", BotID: "B0DEPLOY", Username: "deploybot", Text: text,
			Attachments: fmt.Appendf(nil, `[{"color":%q,"title":%q,"text":%q,"footer":"deploybot"}]`, color, title, body)}
	}

	// fill gives c's messages times nine minutes apart, the newest ago
	// before now, and leaves the newest unread of them unread.
	fill := func(id string, ago time.Duration, unread int, msgs ...slack.Message) []slack.Message {
		c := s.convs[id]
		for i := range msgs {
			msgs[i].TS = stamp(now.Add(-ago - time.Duration(len(msgs)-1-i)*9*time.Minute))
		}
		c.msgs = msgs
		c.LastRead = msgs[len(msgs)-1-unread].TS
		return msgs
	}

	edited := say(Self, "looks fine to me. _one_ question: why not an LRU from the stdlib?")
	edited.Edited = &struct{}{}
	table := say(Jo, "benchmarks from last night:\n| case | before | after |\n|---|---|---|\n| cold start | 410ms | 120ms |\n| switch channel | 9ms | 3ms |\n| idle cpu | 0.6% | 0.1% |")
	table.Reactions = []slack.Reaction{{Name: "rocket", Count: 2, Users: []string{Priya, Tomas}}, {Name: "eyes", Count: 1, Users: []string{Self}}}
	dev := fill(Dev, 20*time.Minute, 0,
		say(Jo, "morning. the cache rewrite is up for review: <https://github.com/example/loafer/pull/42|#42>"),
		say(Priya, "here's the bit I'm least sure of:\n```go\nfunc (s *Store) touch(conv string) {\n\ts.clock++\n\tif w := s.windows[conv]; w != nil {\n\t\tw.used = s.clock\n\t}\n}\n```"),
		edited,
		say(Tomas, "there isn't one, which is the answer to most questions like that"),
		table,
		alert("deployed `loafer@4f2a9c1` to staging", "#2eb67d", "staging", "3 services, none failed"),
	)
	parent := dev[3].TS
	sec, us, _ := strings.Cut(parent, ".")
	at := time.Unix(atoi(sec), atoi(us)*1000)
	for i, r := range []slack.Message{say(Jo, "ha"), say(Priya, "fair. *container/list* it is, then"), say(Self, "noted")} {
		r.TS, r.ThreadTS = stamp(at.Add(time.Duration(i+1)*time.Minute)), parent
		s.add(s.convs[Dev], r)
	}

	welcome := say(Priya, "welcome to the new folks :wave:")
	welcome.Reactions = []slack.Reaction{{Name: "wave", Count: 3, Users: []string{Jo, Tomas, Self}}}
	fill(General, 5*time.Minute, 1,
		welcome,
		say(Jo, "lunch is at 12:30, the place with the good sourdough"),
		say(Self, "I'll be there"),
		say(Tomas, "<@"+Self+"> could you bring the projector cable back when you're in? it's been a week"),
	)

	lines := []string{
		"has anyone tried the new coffee place on the corner",
		"it's fine. the old one was better",
		"bread update: the starter lives",
		"who keeps leaving the window open",
		"me. it's warm",
		"link of the day: <https://example.com/loaves|a short history of the loaf>",
		"lunch?",
		"already ate, sorry",
		"the plant by the door needs water",
		"watered",
		"drinks on friday, usual place",
		"in :tada:",
	}
	who := []string{Jo, Priya, Tomas, Self}
	var random []slack.Message
	for i := range 120 { // more than a page, so scrolling up fetches the rest
		random = append(random, say(who[i%len(who)], lines[i%len(lines)]))
	}
	random[len(random)-1].User = Tomas // the unread ones aren't yours
	fill(Random, 40*time.Minute, 3, random...)

	fill(Alerts, 2*time.Hour, 0,
		alert("staging is slow", "#ecb22e", "staging: p95 latency", "340ms, over the 200ms budget"),
		alert("staging is fine again", "#2eb67d", "staging: p95 latency", "180ms"),
	)
	fill(Design, 3*time.Hour, 0,
		say(Priya, "new icons are in the file. the loaf one especially"),
		say(Jo, "the loaf one is perfect"),
	)
	fill(Pub, 4*time.Hour, 0, say(Jo, "pub on friday?"), say(Priya, "yes"), say(Self, "yes"))
	fill(PriyaDM, 12*time.Minute, 1,
		say(Self, "did you get the logs?"),
		say(Priya, "got them, thanks. got a minute later? the sidebar order looks off on mine"),
	)
	fill(JoDM, 90*time.Minute, 0, say(Self, "pushed the fix"), say(Jo, "ta"))
	deployed := say(DeployBot, "your deploy to *staging* finished in 2m 14s")
	deployed.BotID = "B0DEPLOY"
	ask := say(DeployBot, "loafer@4f2a9c1 is waiting to go to production")
	ask.BotID, ask.AppID = "B0DEPLOY", "A0DEPLOY"
	ask.Blocks = []byte(`[{"type":"section","block_id":"ask","text":{"type":"mrkdwn","text":"*loafer@4f2a9c1* is waiting to go to production"}},
	 {"type":"actions","block_id":"go","elements":[
	  {"type":"button","action_id":"review","style":"primary","text":{"type":"plain_text","text":"Review"},"value":"4f2a9c1"},
	  {"type":"button","action_id":"hold","text":{"type":"plain_text","text":"Not today"},"value":"hold"},
	  {"type":"overflow","action_id":"more","options":[{"text":{"type":"plain_text","text":"see the diff"},"value":"diff"},
	   {"text":{"type":"plain_text","text":"page whoever's on call"},"value":"page"}]}]}]`)
	fill(DeployDM, 19*time.Minute, 0, deployed, ask)

	// Channels you're not in: they're in conversations.list, not in boot.
	for _, c := range []slack.Conversation{
		{ID: Books, Name: "bookclub", IsChannel: true, NumMembers: 7, Purpose: slack.Text{Value: "one chapter a fortnight"}},
		{ID: Old, Name: "old-launch", IsChannel: true, IsArchived: true, NumMembers: 3},
	} {
		s.convs[c.ID] = &conv{Conversation: c, replies: map[string][]slack.Message{}}
	}
	fill(Books, 3*time.Hour, 0, say(Jo, "chapter four is the one with the bakery"), say(Priya, "I'm behind, no spoilers"))

	s.last = now.UnixMicro()
}

// stamp is t as a Slack ts.
func stamp(t time.Time) string {
	us := t.UnixMicro()
	return fmt.Sprintf("%d.%06d", us/1e6, us%1e6)
}

func atoi(s string) int64 {
	n, _ := strconv.ParseInt(s, 10, 64)
	return n
}
