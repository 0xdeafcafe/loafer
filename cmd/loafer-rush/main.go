// Command loafer-rush is loafer as a rush plugin: Slack for the agents
// you run in rush, read through loafer's own sign-in (docs/rush-plugin.md).
// It searches, reads conversations and threads, lists what's unread, looks
// people up, and leaves drafts in loafer's composer. Nothing it does sends,
// reacts or marks anything read: what goes out under your name is yours.
//
// It runs in rush's sandbox, so it reaches Slack only through rush's proxy
// and has rush run the two programs it needs: security(1), for loafer's
// Keychain item, and `loafer draft`, which hands a draft to the running
// loafer.
//
//	go install ./cmd/loafer ./cmd/loafer-rush
//	loafer rush install
package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime/debug"
	"strconv"
	"sync"
	"time"

	"github.com/0xdeafcafe/loafer/internal/slack"
	"github.com/0xdeafcafe/photon/jsonx"
)

func main() {
	f := os.NewFile(3, "rush")
	if f == nil {
		fmt.Fprintln(os.Stderr, "run me from rush: I talk on fd 3")
		os.Exit(2)
	}
	debug.SetMemoryLimit(64 << 20) // under the manifest's 96 MB
	// Go asks trustd about certificates, which the sandbox can't reach;
	// the system's bundle it may read.
	if pem, err := os.ReadFile("/private/etc/ssl/cert.pem"); err == nil {
		if pool := x509.NewCertPool(); pool.AppendCertsFromPEM(pem) {
			http.DefaultTransport.(*http.Transport).TLSClientConfig = &tls.Config{RootCAs: pool}
		}
	}
	c := newConn(f)
	rush, slack.Secret = c, secret
	_ = c.serve(handle)
}

func handle(ctx context.Context, method string, params jsontext.Value) (any, error) {
	switch method {
	case "initialize":
		return map[string]any{}, nil
	case "tools.list":
		return map[string]any{"tools": tools}, nil
	case "tools.call":
		var in struct {
			Name      string         `json:"name"`
			Arguments jsontext.Value `json:"arguments"`
		}
		if err := jsonx.Unmarshal(params, &in); err != nil {
			return nil, err
		}
		ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
		text, err := call(ctx, in.Name, in.Arguments)
		if err != nil {
			return result(err.Error(), true), nil
		}
		return result(text, false), nil
	}
	return nil, &rpcError{Code: -32601, Message: "method not found: " + method}
}

func result(text string, isErr bool) map[string]any {
	return map[string]any{"content": []map[string]any{{"type": "text", "text": text}}, "isError": isErr}
}

// --- rush's protocol: JSON-RPC 2.0 on fd 3, each message behind its
// length as 4 big-endian bytes, requests both ways ---

// caller calls rush. Tests put a fake in rush.
type caller interface {
	Call(ctx context.Context, method string, params, out any) error
}

var rush caller

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *rpcError) Error() string { return e.Message }

type message struct {
	JSONRPC string         `json:"jsonrpc"`
	ID      jsontext.Value `json:"id,omitzero"`
	Method  string         `json:"method,omitzero"`
	Params  jsontext.Value `json:"params,omitzero"`
	Result  jsontext.Value `json:"result,omitzero"`
	Error   *rpcError      `json:"error,omitzero"`
}

type conn struct {
	rw  io.ReadWriter
	wmu sync.Mutex

	mu      sync.Mutex
	next    int64
	pending map[string]chan message
}

func newConn(rw io.ReadWriter) *conn { return &conn{rw: rw, pending: map[string]chan message{}} }

func (c *conn) send(m message) error {
	m.JSONRPC = "2.0"
	b, err := jsonx.Marshal(m)
	if err != nil {
		return err
	}
	buf := binary.BigEndian.AppendUint32(make([]byte, 0, 4+len(b)), uint32(len(b)))
	c.wmu.Lock()
	defer c.wmu.Unlock()
	_, err = c.rw.Write(append(buf, b...))
	return err
}

// serve reads until rush hangs up: replies go to their calls, requests to
// handle, each on its own goroutine, and notifications to handle in order.
func (c *conn) serve(handle func(context.Context, string, jsontext.Value) (any, error)) error {
	for {
		var n [4]byte
		if _, err := io.ReadFull(c.rw, n[:]); err != nil {
			return err
		}
		size := binary.BigEndian.Uint32(n[:])
		if size > 16<<20 {
			return errors.New("a frame past rush's 16 MB")
		}
		b := make([]byte, size)
		if _, err := io.ReadFull(c.rw, b); err != nil {
			return err
		}
		var m message
		if jsonx.Unmarshal(b, &m) != nil {
			continue
		}
		switch {
		case m.Method == "":
			c.mu.Lock()
			ch := c.pending[string(m.ID)]
			delete(c.pending, string(m.ID))
			c.mu.Unlock()
			if ch != nil {
				ch <- m
			}
		case len(m.ID) == 0:
			_, _ = handle(context.Background(), m.Method, m.Params)
		default:
			go func() {
				out, err := handle(context.Background(), m.Method, m.Params)
				r := message{ID: m.ID}
				if err != nil {
					e, ok := errors.AsType[*rpcError](err)
					if !ok {
						e = &rpcError{Code: -32602, Message: err.Error()}
					}
					r.Error = e
				} else if r.Result, err = jsonx.Marshal(out); err != nil {
					r.Error = &rpcError{Code: -32000, Message: err.Error()}
				}
				_ = c.send(r)
			}()
		}
	}
}

// Call calls method and decodes rush's result into out, which may be nil.
func (c *conn) Call(ctx context.Context, method string, params, out any) error {
	p, err := jsonx.Marshal(params)
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.next++
	id := strconv.FormatInt(c.next, 10)
	ch := make(chan message, 1)
	c.pending[id] = ch
	c.mu.Unlock()
	if err := c.send(message{ID: jsontext.Value(id), Method: method, Params: p}); err != nil {
		return err
	}
	select {
	case m := <-ch:
		if m.Error != nil {
			return m.Error
		}
		if out == nil {
			return nil
		}
		return jsonx.Unmarshal(m.Result, out)
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return ctx.Err()
	}
}
