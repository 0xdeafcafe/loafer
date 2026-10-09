package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/0xdeafcafe/loafer/internal/notifyd"
	"github.com/0xdeafcafe/loafer/internal/rushlink"
	"github.com/0xdeafcafe/loafer/internal/slack"
)

// rushInstall is `loafer rush install`: loafer's rush plugin into rush's
// plugin folder, then rush's approval, which says what it may do.
func rushInstall() error {
	rush, err := rushlink.Find()
	if err != nil {
		return err
	}
	exe, err := os.Executable()
	if err == nil {
		exe, err = filepath.EvalSymlinks(exe)
	}
	if err != nil {
		return err
	}
	plugin := filepath.Join(filepath.Dir(exe), "loafer-rush")
	if _, err := os.Stat(plugin); err != nil {
		return errors.New("loafer-rush isn't beside loafer: go install github.com/0xdeafcafe/loafer/cmd/loafer-rush@latest")
	}
	ws, err := slack.Workspaces()
	if err != nil {
		return err
	}
	if len(ws) == 0 {
		return errors.New("sign in first: loafer login")
	}
	if err := rush.Install(plugin, exe, ws[0].URL); err != nil {
		return err
	}
	fmt.Println("installed; rush asks you to approve it now")
	approve := exec.Command(rush.Path, "plugin", "approve", "loafer")
	approve.Stdin, approve.Stdout, approve.Stderr = os.Stdin, os.Stdout, os.Stderr
	return approve.Run()
}

// offerRush asks, once, whether to install the rush plugin, when rush is
// here and the plugin isn't.
func offerRush() {
	asked := filepath.Join(notifyd.Dir(), "rush-asked")
	if _, err := os.Stat(asked); err == nil {
		return
	}
	rush, err := rushlink.Find()
	if err != nil || rush.Installed() {
		return
	}
	_ = os.MkdirAll(notifyd.Dir(), 0o700)
	_ = os.WriteFile(asked, nil, 0o600)
	fmt.Print("rush is here. give your agents slack (search, read, drafts; never sending) with loafer's rush plugin? [Y/n] ")
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	if a := strings.ToLower(strings.TrimSpace(line)); a != "" && a != "y" && a != "yes" {
		fmt.Println("skipped; loafer rush install does it later")
		return
	}
	if err := rushInstall(); err != nil {
		fmt.Fprintln(os.Stderr, "loafer:", err)
	}
}
