package slacktest

import (
	"context"
	"fmt"
	"math/rand/v2"
	"slices"
	"strconv"
	"time"

	"github.com/0xdeafcafe/loafer/internal/slack"
)

// The big workspace: the small team inside a company of 20,000, with
// 2,000 channels (you're in 300), 400 DMs, sections hundreds long and one
// channel, #firehose, 100,000 messages deep. It's made from fixed seeds,
// so every run meets the same company.

// Big is #firehose.
const Big = "C0BIG"

const (
	BigPeople   = 20000
	BigChannels = 2000
	BigJoined   = 300
	BigDMs      = 400
	BigHistory  = 100_000
)

var (
	firsts = []string{"ada", "ben", "cleo", "dev", "eli", "fern", "gus", "hana", "ivo", "june",
		"kai", "lena", "milo", "nia", "otto", "pia", "quinn", "rosa", "sol", "tess",
		"uma", "vik", "wren", "xan", "yara", "zed", "ari", "bo", "cy", "dot"}
	lasts = []string{"abbott", "baker", "chen", "dube", "evans", "fox", "garcia", "hill", "ito", "jones",
		"khan", "lopez", "mills", "novak", "okoro", "park", "quist", "rossi", "silva", "tran",
		"usman", "vega", "wong", "xu", "young", "zhou", "adeyemi", "berg", "costa", "diaz"}
	teams = []string{"eng", "ops", "proj", "help", "sales", "inc", "feat", "ext", "loc", "team"}
	words = []string{"payments", "search", "mobile", "infra", "billing", "growth", "auth", "data", "web", "api",
		"ios", "android", "design", "docs", "oncall", "deploys", "perf", "cache", "ledger", "ingest"}
	chatter = []string{
		"morning all",
		"has anyone looked at the flaky test in *ledger*? it's failing one run in five",
		"pushed a fix: <https://github.com/example/ledger/pull/1234|#1234>",
		"`make test` is green for me locally",
		"lgtm :shipit:",
		"can we move standup to 10:15 tomorrow",
		"the dashboard's here <https://grafana.example.com/d/abc|p95 latency>, it's back under budget",
		"thread for the incident review, please :thread:",
		"> quoting the spec: _retries are idempotent_\nwhich they aren't, quite",
		"```go\nfor i := range n {\n\tif err := step(i); err != nil {\n\t\treturn err\n\t}\n}\n```",
		"on it",
		"lunch? the noodle place",
		"reminder: the freeze starts friday at 17:00",
		"1. pull main\n2. run the migration\n3. tell #deploys",
		"I think that's the cache again, not the network",
		"thanks, that did it :tada:",
	}
	reactions = []string{"eyes", "+1", "tada", "rocket", "white_check_mark", "joy"}
)

func bigPerson(i int) string { return fmt.Sprintf("UB%05d", i) }

func (s *Server) seedBig(now time.Time) {
	rng := rand.New(rand.NewPCG(1, 2))
	for i := range BigPeople {
		first, last := firsts[i%len(firsts)], lasts[i/len(firsts)%len(lasts)]
		u := slack.User{ID: bigPerson(i), Name: first + "." + last + strconv.Itoa(i/900), Color: fmt.Sprintf("%06x", rng.IntN(1<<24)), TZ: "Europe/London"}
		u.RealName = first + " " + last
		u.Profile.RealName, u.Profile.DisplayName = u.RealName, first
		u.Deleted = i%97 == 0
		if i%500 == 499 {
			u.IsBot, u.Profile.BotID, u.Profile.DisplayName = true, "BB"+strconv.Itoa(i), first+"bot"
		}
		s.users = append(s.users, u)
	}
	someone := func() string { return bigPerson(rng.IntN(BigPeople)) }
	say := func(at time.Time) slack.Message {
		m := slack.Message{Type: "message", TS: stamp(at), User: someone(), Text: chatter[rng.IntN(len(chatter))]}
		if rng.IntN(25) == 0 {
			m.Reactions = []slack.Reaction{{Name: reactions[rng.IntN(len(reactions))], Count: 2, Users: []string{someone(), someone()}}}
		}
		return m
	}
	add := func(c slack.Conversation) *conv {
		v := &conv{Conversation: c, replies: map[string][]slack.Message{}}
		s.convs[c.ID] = v
		s.order = append(s.order, c.ID)
		return v
	}
	// fill gives c n messages from ago back, oldest first, and leaves
	// unread of them unread.
	fill := func(c *conv, n, unread int, ago, gap time.Duration) {
		for i := range n {
			c.msgs = append(c.msgs, say(now.Add(-ago-time.Duration(n-1-i)*gap)))
		}
		c.LastRead = "0000000000.000000"
		if unread < n {
			c.LastRead = c.msgs[n-1-unread].TS
		}
	}

	var joined []string
	for i := range BigChannels {
		name := teams[i%len(teams)] + "-" + words[i/len(teams)%len(words)]
		if i >= len(teams)*len(words) {
			name += "-" + strconv.Itoa(i/(len(teams)*len(words)))
		}
		c := slack.Conversation{ID: fmt.Sprintf("CB%04d", i), Name: name, IsChannel: true, IsMember: i < BigJoined,
			NumMembers: 3 + rng.IntN(800), IsPrivate: i%11 == 0}
		if i%3 == 0 {
			c.Topic.Value = "all things " + words[i/len(teams)%len(words)] + ", " + teams[i%len(teams)] + " side"
		}
		v := add(c)
		if c.IsMember {
			unread := 0
			if i%5 == 0 {
				unread = 1 + rng.IntN(3)
			}
			fill(v, 6, unread, time.Duration(rng.IntN(30*24*60))*time.Minute, 7*time.Minute)
			if i%40 == 0 { // now and then, one names you
				v.msgs[len(v.msgs)-1].Text = "<@" + Self + "> can you take a look?"
			}
			joined = append(joined, c.ID)
		}
	}
	for i := range BigDMs {
		v := add(slack.Conversation{ID: fmt.Sprintf("DB%04d", i), IsIM: true, IsOpen: true, User: bigPerson(i * (BigPeople / BigDMs))})
		unread := 0
		if i%25 == 0 {
			unread = 1
		}
		fill(v, 3, unread, time.Duration(rng.IntN(60*24*60))*time.Minute, 3*time.Minute)
		for j := range v.msgs { // a DM is the two of you
			if j%2 == 1 {
				v.msgs[j].User = Self
			} else {
				v.msgs[j].User = v.User
			}
		}
	}

	big := add(slack.Conversation{ID: Big, Name: "firehose", IsChannel: true, IsMember: true, NumMembers: BigPeople,
		Topic: slack.Text{Value: "everything, all at once"}})
	big.msgs = make([]slack.Message, 0, BigHistory)
	fill(big, BigHistory, 50, time.Minute, 30*time.Second)

	// Sections: stars and two of your own, hundreds long, between the
	// small team's Team and Channels.
	byID := map[string]*slack.Section{}
	for i := range s.sections {
		byID[s.sections[i].ID] = &s.sections[i]
	}
	stars := byID["S0STARS"]
	stars.Channels.IDs = append(stars.Channels.IDs, Big)
	stars.Channels.IDs = append(stars.Channels.IDs, joined[:20]...)
	byID["S0TEAM"].Next = "SBPROJ"
	proj := slack.Section{ID: "SBPROJ", Name: "Projects", Type: "standard", Next: "SBPEOPLE"}
	proj.Channels.IDs = joined[20:170]
	people := slack.Section{ID: "SBPEOPLE", Name: "People", Type: "standard", Emoji: ":wave:", Next: "S0CHANNELS"}
	for i := range 120 {
		people.Channels.IDs = append(people.Channels.IDs, fmt.Sprintf("DB%04d", i))
	}
	s.sections = append(s.sections, proj, people)
}

// Stream pushes about perSec events a second until ctx ends, as a busy
// workspace sends them: messages across the conversations you're in,
// reactions to them, people typing, and presence and profile noise. Each
// run sends the same events in the same order.
func (s *Server) Stream(ctx context.Context, perSec int) {
	rng := rand.New(rand.NewPCG(3, 4))
	var ids []string
	s.mu.Lock()
	for _, id := range s.order {
		if c := s.convs[id]; c.IsMember || c.IsIM {
			ids = append(ids, id)
		}
	}
	people := len(s.users)
	s.mu.Unlock()
	someone := func() string { return s.users[rng.IntN(people)].ID } // users isn't changed after seeding
	t := time.NewTicker(time.Second / time.Duration(perSec))
	defer t.Stop()
	for i := 0; ; i++ {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		conv := ids[rng.IntN(len(ids))]
		switch k := i % 20; {
		case i%100 == 99:
			s.mu.Lock()
			u := s.users[rng.IntN(people)]
			u.Profile.StatusEmoji, u.Profile.StatusText = ":coffee:", "back in "+strconv.Itoa(i%60)+" minutes"
			s.pushAny(map[string]any{"type": "user_change", "user": u})
			s.mu.Unlock()
		case k < 10:
			s.Post(conv, someone(), chatter[rng.IntN(len(chatter))], "")
		case k < 14:
			s.react(conv, someone(), reactions[rng.IntN(len(reactions))])
		case k < 18:
			s.Typing(conv, someone())
		default:
			s.Push(fmt.Sprintf(`{"type":"presence_change","users":[%q,%q,%q],"presence":%q}`, someone(), someone(), someone(), []string{"active", "away"}[i%2]))
		}
	}
}

// react has user add name to conv's newest message, and tells the sockets.
func (s *Server) react(conv, user, name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := s.convs[conv]
	if c == nil || len(c.msgs) == 0 {
		return
	}
	m := &c.msgs[len(c.msgs)-1]
	i := slices.IndexFunc(m.Reactions, func(r slack.Reaction) bool { return r.Name == name })
	switch {
	case i < 0:
		m.Reactions = append(m.Reactions, slack.Reaction{Name: name, Count: 1, Users: []string{user}})
	case slices.Contains(m.Reactions[i].Users, user):
		return
	default:
		m.Reactions[i].Count++
		m.Reactions[i].Users = append(m.Reactions[i].Users, user)
	}
	s.pushAny(map[string]any{"type": "reaction_added", "user": user, "reaction": name,
		"item": map[string]string{"type": "message", "channel": conv, "ts": m.TS}, "event_ts": s.ts()})
}
