package slack

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"time"

	"github.com/0xdeafcafe/photon/jsonx"
)

// MaxUpload is the most Slack takes in one file: 1 GB.
const MaxUpload = 1 << 30

// UploadFile is a file on disk to send.
type UploadFile struct {
	Path string
	Name string // as it'll be called in Slack
	Size int64
}

// Share is where uploaded files go, and what's said with them.
type Share struct {
	Channel  string
	ThreadTS string // a reply in this thread, if set
	Text     string // the comment that goes with them
	Also     bool   // a thread reply that's sent to the channel too
}

// Upload sends files by Slack's current flow: files.getUploadURLExternal
// for a place to put each, the bytes POSTed there straight from disk, then
// one files.completeUploadExternal to share them all. The message arrives
// over the websocket like any other. progress hears the bytes sent so far
// and the total, as the body is read.
//
// The shapes are from Slack's docs (docs/slack-webapp-methods.md); that the
// web client's xoxc- session is taken by these methods, as it is by the
// older files.upload, is unchecked.
func (c *Client) Upload(ctx context.Context, to Share, files []UploadFile, progress func(sent, total int64)) error {
	var total, sent int64
	for _, f := range files {
		total += f.Size
	}
	var done []map[string]string
	for _, f := range files {
		var r struct {
			URL string `json:"upload_url"`
			ID  string `json:"file_id"`
		}
		err := c.Call(ctx, "files.getUploadURLExternal", url.Values{"filename": {f.Name}, "length": {strconv.FormatInt(f.Size, 10)}}, &r)
		if err != nil {
			return err
		}
		if r.URL == "" || r.ID == "" {
			return &Error{Method: "files.getUploadURLExternal", Code: "no_upload_url"}
		}
		if err := c.send(ctx, r.URL, f, func(n int64) { progress(sent+n, total) }); err != nil {
			return err
		}
		sent += f.Size
		done = append(done, map[string]string{"id": r.ID, "title": f.Name})
	}
	list, err := jsonx.Marshal(done)
	if err != nil {
		return err
	}
	form := url.Values{"files": {string(list)}, "channel_id": {to.Channel}}
	if to.Text != "" {
		form.Set("initial_comment", to.Text)
	}
	if to.ThreadTS != "" {
		form.Set("thread_ts", to.ThreadTS)
		if to.Also {
			form.Set("reply_broadcast", "true")
		}
	}
	return c.Call(ctx, "files.completeUploadExternal", form, nil)
}

// send POSTs f's bytes to u, which is Slack's and carries its own
// authority, so no cookie goes with it. The URL isn't logged either.
func (c *Client) send(ctx context.Context, u string, f UploadFile, on func(int64)) error {
	file, err := os.Open(f.Path)
	if err != nil {
		return err
	}
	defer file.Close()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, &counted{r: file, on: on})
	if err != nil {
		return err
	}
	req.ContentLength = f.Size // a body of unknown length would go chunked
	req.Header.Set("Content-Type", "application/octet-stream")
	began := time.Now()
	resp, err := c.patient().Do(req)
	if err != nil {
		slog.Warn("upload", "err", err.Error())
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<10))
	host := req.URL.Hostname()
	if resp.StatusCode != http.StatusOK {
		slog.Warn("upload", "host", host, "status", resp.StatusCode)
		return &Error{Method: "POST " + host, Status: resp.StatusCode}
	}
	slog.Info("upload", "host", host, "ms", time.Since(began).Milliseconds(), "bytes", f.Size)
	return nil
}

// counted tells on how much of r has been read.
type counted struct {
	r  io.Reader
	n  int64
	on func(int64)
}

func (c *counted) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	c.on(c.n)
	return n, err
}
