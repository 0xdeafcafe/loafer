package slacktest

import (
	"context"
	"sync"
)

// Hold has calls to method wait, as a slow Slack's would, until the func
// it returns is called: for seeing a cold start's welcome advance, in
// tests and the demo.
func (s *Server) Hold(method string) (release func()) {
	ch := make(chan struct{})
	s.mu.Lock()
	if s.holds == nil {
		s.holds = map[string]chan struct{}{}
	}
	s.holds[method] = ch
	s.mu.Unlock()
	var once sync.Once
	return func() { once.Do(func() { close(ch) }) }
}

// held waits while method is held.
func (s *Server) held(ctx context.Context, method string) {
	s.mu.Lock()
	ch := s.holds[method]
	s.mu.Unlock()
	if ch != nil {
		select {
		case <-ch:
		case <-ctx.Done():
		}
	}
}
