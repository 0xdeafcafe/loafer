package notifyd

import (
	"bufio"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// The handover is two files in the run directory:
//   - tui.lock, which every running TUI holds shared for as long as it
//     lives. notifyd waits for it exclusively, so the kernel tells it when
//     the last TUI has gone, crashes and all, with nothing polled.
//   - notify.sock, where a starting TUI says "take", and notifyd answers
//     "parked" once its websocket is closed. Only then does the TUI open
//     its own, so there's one connection at a time.
//
// notifyd.lock keeps notifyd to one of itself.

// Dir is where the socket and the locks live: ~/.loafer/run.
func Dir() string {
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".loafer", "run")
}

func sockPath(dir string) string { return filepath.Join(dir, "notify.sock") }
func tuiLock(dir string) string  { return filepath.Join(dir, "tui.lock") }

// Take has notifyd, if it's running, put its websocket down until this
// process's release (or exit), and returns once it has. Without notifyd
// it only takes the lock, so a notifyd started later waits too.
func Take(dir string) (release func(), err error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return func() {}, err
	}
	// Blocks only while notifyd or status looks, which is a moment.
	f, err := lock(tuiLock(dir), syscall.LOCK_SH)
	if err != nil {
		return func() {}, err
	}
	release = func() { f.Close() } // closing it lets go of the lock
	switch reply, err := Ask(dir, "take"); {
	case errors.Is(err, ErrNotRunning):
		return release, nil
	case err != nil:
		return release, err
	case reply != "parked":
		return release, errors.New("notifyd answered " + reply)
	}
	return release, nil
}

// ErrNotRunning is Ask's when nothing answers on the socket.
var ErrNotRunning = errors.New("notifyd isn't running")

// Ask says one word to notifyd and gives its one-line answer.
func Ask(dir, what string) (string, error) {
	c, err := net.DialTimeout("unix", sockPath(dir), time.Second)
	if err != nil {
		return "", ErrNotRunning // no socket, or one left by a notifyd that's gone
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := c.Write([]byte(what + "\n")); err != nil {
		return "", err
	}
	line, err := bufio.NewReader(c).ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

// TUI says whether a TUI is running, by whether it holds its lock.
func TUI(dir string) bool { return held(tuiLock(dir)) }

// lock opens path and flocks it how, blocking unless how has LOCK_NB.
func lock(path string, how int) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), how); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

// held says whether someone holds path's lock, taking it for no longer
// than it takes to look.
func held(path string) bool {
	f, err := lock(path, syscall.LOCK_EX|syscall.LOCK_NB)
	if err != nil {
		return errors.Is(err, syscall.EWOULDBLOCK)
	}
	f.Close()
	return false
}
