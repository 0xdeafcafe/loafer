package ui

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/png"
	"slices"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/loafer/internal/images"
	"github.com/0xdeafcafe/loafer/internal/slack"
	"github.com/0xdeafcafe/loafer/internal/slacktest"
	"github.com/0xdeafcafe/loafer/internal/store"
	"github.com/0xdeafcafe/photon/termimg"
)

// bigPics gives r's model pictures from memory, each a millisecond on
// its way, and every person in the big workspace an avatar, as users.list
// does on a first load.
func bigPics(tb testing.TB, r *rig) {
	var b bytes.Buffer
	png.Encode(&b, image.NewNRGBA(image.Rect(0, 0, 72, 72)))
	was := termimg.Current
	termimg.Current = termimg.Kitty
	termimg.SetCell(10, 20)
	pics = images.New(r.ctx, func(ctx context.Context, u string) ([]byte, error) {
		time.Sleep(time.Millisecond)
		return b.Bytes(), nil
	}, "")
	tb.Cleanup(func() { pics, termimg.Current = nil, was })
	people := make([]slack.User, slacktest.BigPeople)
	for i := range people {
		people[i].ID = fmt.Sprintf("UB%05d", i)
		people[i].Name = fmt.Sprintf("person%d", i)
		people[i].Profile.Image72 = fmt.Sprintf("https://avatars.example/%d.png", i)
	}
	r.m.st.ApplyPeople(people)
	r.run(r.m.waitPics())
}

// TestPictureBurst scrolls #firehose back a page at a time on a first
// load, so hundreds of avatars land at once, and counts what their
// landing costs the UI goroutine: the landings it hears of and the time
// it spends in Update and View. Each landing draws the screen again, so
// they're heard of a few at a time, not one by one.
func TestPictureBurst(t *testing.T) {
	if testing.Short() {
		t.Skip("boots the big workspace")
	}
	srv := slacktest.NewBig()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel(); srv.Close() })
	r := newRig(t, ctx, srv, store.New())
	r.run(r.m.Init())
	r.untilLive()
	r.run(r.m.visit(slacktest.Big))
	r.until("#firehose", 5*time.Second, func() bool { return r.held(slacktest.Big) })
	r.settle()
	bigPics(t, r)
	r.m.setFocus(onMsgs)

	r.reset()
	landings, raw, began := 0, 0, time.Now()
	for range 20 {
		r.step(tea.KeyPressMsg{Code: tea.KeyPgUp})
	}
	quiet := time.After(time.Second)
	for wait := true; wait; {
		select {
		case msg := <-r.msgs:
			switch msg := msg.(type) {
			case picsMsg:
				landings++
			case tea.RawMsg:
				raw += len(fmt.Sprint(msg.Msg))
			}
			r.step(msg)
			quiet = time.After(300 * time.Millisecond)
		case <-quiet:
			wait = false
		}
	}
	var busy time.Duration
	for _, d := range append(slices.Clone(r.keys), r.frames...) {
		busy += d
	}
	f := slices.Sorted(slices.Values(r.frames))
	t.Logf("%d landings heard in %s, %d frames (p50 %s, p99 %s, max %s), UI busy %s, %d KB raw",
		landings, time.Since(began).Round(time.Millisecond), len(f), ms(pct(f, 50)), ms(pct(f, 99)), ms(f[len(f)-1]), ms(busy), raw>>10)
}
