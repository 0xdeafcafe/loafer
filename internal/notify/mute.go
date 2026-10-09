package notify

import (
	"slices"
	"strings"
)

// Muted says whether conv is muted, by either of the places Slack may keep
// it (muted_channels, and the per-conversation settings, a guess).
func (p *Prefs) Muted(conv string) bool { return p.muted[conv] || p.chans[conv].muted }

// Mute mutes or unmutes conv here, and returns what muted_channels should
// now be, to send with users.prefs.set.
func (p *Prefs) Mute(conv string, on bool) string {
	if p.muted == nil {
		p.muted = set{}
	}
	if on {
		p.muted[conv] = true
	} else {
		delete(p.muted, conv)
	}
	if c, ok := p.chans[conv]; ok && !on {
		c.muted = false
		p.chans[conv] = c
	}
	var ids []string
	for id := range p.muted {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return strings.Join(ids, ",")
}
