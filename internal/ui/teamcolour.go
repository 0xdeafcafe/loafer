package ui

import (
	"hash/fnv"
	"strconv"

	"github.com/0xdeafcafe/loafer/internal/slack"
	"github.com/0xdeafcafe/photon/theme"
)

// A workspace.s own colour: its chip on the workspace rail (docs/ui.md,
// rule 2a). Nothing else is painted in it.

// wsColours are what a workspace without a theme of its own gets, by a
// hash of its id: Slack's theme colours and a few more like them, all
// dark enough for the header's text.
var wsColours = []theme.RGB{
	rgb(0x0d, 0x7e, 0x83), rgb(0x30, 0x3e, 0x4d), rgb(0x54, 0x45, 0x38), rgb(0x1f, 0x5c, 0x45),
	rgb(0x7a, 0x2e, 0x1f), rgb(0x1d, 0x3b, 0x6e), rgb(0x6b, 0x4a, 0x0e), rgb(0x5a, 0x2a, 0x6e),
}

// teamColour is t's colour: its sidebar theme's when Slack gave one, else
// one of wsColours that stays the same for it.
func teamColour(t slack.Team) theme.RGB {
	if len(t.Colour) == 7 {
		if v, err := strconv.ParseUint(t.Colour[1:], 16, 32); err == nil {
			return rgb(uint8(v>>16), uint8(v>>8), uint8(v))
		}
	}
	h := fnv.New32a()
	h.Write([]byte(t.ID))
	return wsColours[h.Sum32()%uint32(len(wsColours))]
}


// railInk is text that reads on c: the header's on a dark colour, dark
// text on a light one.
func railInk(c theme.RGB) theme.RGB {
	if c.Dark() {
		return rgb(240, 236, 228)
	}
	return theme.Light.FG
}
