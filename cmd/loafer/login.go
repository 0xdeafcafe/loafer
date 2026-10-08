package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/x/term"

	"github.com/0xdeafcafe/loafer/internal/slack"
)

const loginHelp = `Sign in with your Slack session. Open Slack in a browser (app.slack.com),
sign in, then open DevTools:

  token   Console:  JSON.parse(localStorage.localConfig_v2).teams[Object.keys(JSON.parse(localStorage.localConfig_v2).teams)[0]].token
          (it starts xoxc-; with several workspaces, pick the team id you want)
  cookie  Application → Cookies → https://app.slack.com → d  (it starts xoxd-)

Both stay on this Mac, in your login Keychain. Neither is shown as you paste.
`

// login asks for the token and d cookie, checks them with auth.test, and
// keeps them in the Keychain.
func login() error {
	fmt.Fprint(os.Stderr, loginHelp)
	token, err := ask("token: ")
	if err != nil {
		return err
	}
	cookie, err := ask("cookie: ")
	if err != nil {
		return err
	}
	c := slack.Creds{Token: slack.CleanToken(token), Cookie: slack.CleanCookie(cookie)}
	if !strings.HasPrefix(c.Token, "xoxc-") {
		return fmt.Errorf("the token should start xoxc-")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if c, err = slack.New(c).AuthTest(ctx); err != nil {
		if slack.SignedOut(err) {
			return fmt.Errorf("Slack didn't accept those (%v); check the cookie is the d cookie from the same browser", err)
		}
		return err
	}
	if err := slack.Save(c); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "signed in to %s (%s) as %s\n", c.Team, c.URL, c.UserID)
	return nil
}

var stdin = bufio.NewReader(os.Stdin)

// ask reads one line without echoing it when stdin is a terminal.
func ask(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	if fd := os.Stdin.Fd(); term.IsTerminal(fd) {
		b, err := term.ReadPassword(fd)
		fmt.Fprintln(os.Stderr)
		return string(b), err
	}
	line, err := stdin.ReadString('\n')
	if line != "" {
		err = nil
	}
	return line, err
}
