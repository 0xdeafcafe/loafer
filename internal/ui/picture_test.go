package ui

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi/kitty"

	"github.com/0xdeafcafe/loafer/internal/images"
	"github.com/0xdeafcafe/loafer/internal/slack"
	"github.com/0xdeafcafe/photon/canvas"
	"github.com/0xdeafcafe/photon/termimg"
)

// withPics draws pictures from a server whose answers wait for gate, as
// a Kitty terminal with 10×20 cells would.
func withPics(t *testing.T, gate chan struct{}) *httptest.Server {
	var b bytes.Buffer
	png.Encode(&b, image.NewNRGBA(image.Rect(0, 0, 72, 72)))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-gate
		w.Write(b.Bytes())
	}))
	was := termimg.Current
	termimg.Current = termimg.Kitty
	termimg.SetCell(10, 20)
	pics = images.New(context.Background(), func(ctx context.Context, u string) ([]byte, error) {
		resp, err := http.Get(u)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		return io.ReadAll(resp.Body)
	}, "")
	t.Cleanup(func() { srv.Close(); pics, termimg.Current = nil, was })
	return srv
}

// land waits for a picture to land and lets the model hear of it.
func land(t *testing.T, m *Model) {
	t.Helper()
	select {
	case <-pics.Landed():
		m.onPics(picsMsg{})
	case <-time.After(5 * time.Second):
		t.Fatal("nothing landed")
	}
}

func hasPicture(rows []canvas.Row) bool {
	for _, r := range rows {
		for _, s := range r {
			if strings.ContainsRune(s.Text, kitty.Placeholder) {
				return true
			}
		}
	}
	return false
}

// Without graphics it's initials, as it always was, and nothing's fetched.
func TestPicturesOffKeepsInitials(t *testing.T) {
	m := fixture(t)
	text := strings.Join(plainFrame(m.render()), "\n")
	if !strings.Contains(text, "Dr drew") {
		t.Fatalf("no initials:\n%s", text)
	}
	if hasPicture(m.render()) {
		t.Fatal("a picture with graphics off")
	}
}

// An avatar is 2 cells on the header row once it lands, the initials till
// then, and the frame stays exactly its size.
func TestAvatarLands(t *testing.T) {
	gate := make(chan struct{})
	srv := withPics(t, gate)
	m := fixture(t)
	u := slack.User{ID: "U1", Name: "drew"}
	u.Profile.Image72 = srv.URL + "/drew.png"
	m.st.ApplyPeople([]slack.User{u})
	text := strings.Join(plainFrame(m.render()), "\n")
	if !strings.Contains(text, "Dr drew") {
		t.Fatalf("initials while it's on its way:\n%s", text)
	}
	close(gate)
	land(t, m)
	rows := m.render()
	if !hasPicture(rows) {
		t.Fatal("no avatar after it landed")
	}
	for i, r := range rows {
		if r.Width() != m.w {
			t.Fatalf("row %d is %d wide", i, r.Width())
		}
	}
	if text := strings.Join(plainFrame(rows), "\n"); strings.Contains(text, "Dr drew") {
		t.Fatalf("initials beside the avatar:\n%s", text)
	}
	// Behind ctrl+k, pictures blank: their ids went with their colours.
	m.openJump()
	if hasPicture(m.render()) {
		t.Fatal("a picture behind the overlay")
	}
}

// An image file with its size known holds its room while it's on its way,
// so the rows don't move when it lands.
func TestImageHoldsItsRoom(t *testing.T) {
	gate := make(chan struct{})
	srv := withPics(t, gate)
	m := fixture(t)
	ts := "1999999999.000100"
	files := `[{"name":"cat.png","mimetype":"image/png","mode":"hosted","thumb_720":"` + srv.URL + `/cat.png","original_w":72,"original_h":72}]`
	m.st.SetWindow("C1", []slack.Message{{TS: ts, User: "U1", Text: "look", Files: []byte(files)}}, false, false)
	m.render()
	before := m.heights[ts]
	close(gate)
	land(t, m)
	rows := m.render()
	if !hasPicture(rows) {
		t.Fatal("no picture after it landed")
	}
	if after := m.heights[ts]; after != before || before < 5 {
		t.Fatalf("%d rows on its way, %d landed", before, after)
	}
}
