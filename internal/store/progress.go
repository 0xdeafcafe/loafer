package store

// Boot's steps, as they finish, for the welcome screen a cold start shows
// while there's nothing cached to draw.

// Step is one of Boot's fetches.
type Step uint8

const (
	StepBoot     Step = iota // client.userBoot: you, the team, your conversations
	StepCounts               // client.counts: unread and mentions
	StepPeople               // users.list, every page
	StepSections             // users.channelSections.list
	StepEmoji                // emoji.list
	Steps                    // how many there are
)

// Progress is what Boot has got through so far. A step that failed is
// done too, as far as waiting goes: Boot carries on without it.
type Progress struct {
	Done, Failed uint32 // bits by Step
	People       int    // people held, growing page by page
}

// Has says whether step s is done, well or not.
func (p Progress) Has(s Step) bool { return p.Done&(1<<s) != 0 }

// Bad says whether step s failed.
func (p Progress) Bad(s Step) bool { return p.Failed&(1<<s) != 0 }

// Progress is how far Boot has got.
func (v View) Progress() Progress {
	return Progress{Done: v.s.done, Failed: v.s.failed, People: len(v.s.people)}
}

// stepped marks s done (failed, with err), and says so.
func (s *Store) stepped(st Step, err error) {
	s.update(func() {
		s.done |= 1 << st
		if err != nil {
			s.failed |= 1 << st
		} else {
			s.failed &^= 1 << st
		}
	})
}
