package notify

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
)

// loafer's notifier app (notifier.swift) shows notifications that go
// where they came from when clicked, which osascript's and the
// terminals' can't. It's built here with Xcode's command line tools and
// signed ad hoc, as rush's menu bar app is.

//go:embed notifier.swift
var source string

// The icon (icon/README.md): Assets.car is the Liquid Glass one for
// macOS 26 on, AppIcon.icns the flat one before.
var (
	//go:embed icon/Assets.car
	assetsCar []byte
	//go:embed icon/AppIcon.icns
	appIcns []byte
)

// BundleID is the app's identity: macOS keeps its notification settings
// under it.
const BundleID = "com.github.0xdeafcafe.loafer.notifier"

func appDir() string {
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".loafer")
}

// AppPath is where the built app lives.
func AppPath() string { return filepath.Join(appDir(), "loafer.app") }

// Built says whether the app is there to show notifications with.
func Built() bool {
	_, err := os.Stat(filepath.Join(AppPath(), "Contents", "MacOS", "loafer-notifier"))
	return err == nil
}

// Build builds the app to run bin (loafer) when a notification is
// clicked, if it isn't built from this source for bin already. Builders
// running together take turns.
func Build(bin string) error {
	if runtime.GOOS != "darwin" {
		return errors.New("the notifier is macOS only")
	}
	if err := os.MkdirAll(appDir(), 0o700); err != nil {
		return err
	}
	if f, err := os.OpenFile(filepath.Join(appDir(), "build.lock"), os.O_RDWR|os.O_CREATE, 0o600); err == nil {
		defer f.Close()
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_EX)
	}
	app := AppPath()
	// ponytail: the icons count by size, not hashed, to keep this cheap at every start
	h := sha256.Sum256(fmt.Appendf(nil, "%s\x00%s\x00%d\x00%d", source, bin, len(assetsCar), len(appIcns)))
	stamp := hex.EncodeToString(h[:8])
	stampPath := app + ".stamp" // beside it: inside would break its signature
	if b, _ := os.ReadFile(stampPath); string(b) == stamp && Built() {
		return nil
	}
	if _, err := exec.LookPath("swiftc"); err != nil {
		return errors.New("building the notifier needs the Swift compiler: xcode-select --install")
	}
	tmp, err := os.MkdirTemp("", "loafer-notifier")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	src := filepath.Join(tmp, "main.swift")
	if err := os.WriteFile(src, []byte(source), 0o600); err != nil {
		return err
	}
	_ = os.RemoveAll(app)
	for _, d := range []string{"MacOS", "Resources"} {
		if err := os.MkdirAll(filepath.Join(app, "Contents", d), 0o755); err != nil {
			return err
		}
	}
	for name, b := range map[string][]byte{"Assets.car": assetsCar, "AppIcon.icns": appIcns} {
		if err := os.WriteFile(filepath.Join(app, "Contents", "Resources", name), b, 0o644); err != nil {
			return err
		}
	}
	out, err := exec.Command("swiftc", "-O", "-swift-version", "5", "-parse-as-library", "-o", filepath.Join(app, "Contents", "MacOS", "loafer-notifier"), src).CombinedOutput()
	if err != nil {
		return fmt.Errorf("building the notifier: %v\n%s", err, out)
	}
	if err := os.WriteFile(filepath.Join(app, "Contents", "Info.plist"), fmt.Appendf(nil, plist, BundleID, time.Now().Unix(), xmlEscape(bin)), 0o644); err != nil {
		return err
	}
	// Notifications need a signed app; an ad-hoc signature is enough on
	// the Mac that built it.
	if out, err := exec.Command("/usr/bin/codesign", "--force", "--sign", "-", app).CombinedOutput(); err != nil {
		return fmt.Errorf("signing the notifier: %v\n%s", err, out)
	}
	// Launch Services and Notification Center keep an app's icon by its
	// path and version; registering again has them take this build's.
	_ = exec.Command("/System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Support/lsregister", "-f", app).Run()
	return os.WriteFile(stampPath, []byte(stamp), 0o644)
}

// App shows n through the notifier app, which Build has to have built.
func App(ctx context.Context, n Note) error {
	return exec.CommandContext(ctx, "/usr/bin/open", "-n", "-g", "-a", AppPath(), "--args", "post",
		cut(clean(n.Title), 80), cut(clean(n.Body), 200), n.Team, n.Conv, n.TS, n.Thread).Run()
}

// Show shows n through the notifier app if it's built, else osascript.
func Show(ctx context.Context, n Note) error {
	if Built() {
		return App(ctx, n)
	}
	return Osascript(ctx, n)
}

func xmlEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}

const plist = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleIdentifier</key><string>%s</string>
	<key>CFBundleName</key><string>loafer</string>
	<key>CFBundleDisplayName</key><string>loafer</string>
	<key>CFBundleExecutable</key><string>loafer-notifier</string>
	<key>CFBundleIconName</key><string>Loafer</string>
	<key>CFBundleIconFile</key><string>AppIcon</string>
	<key>CFBundlePackageType</key><string>APPL</string>
	<key>CFBundleShortVersionString</key><string>1.0</string>
	<key>CFBundleVersion</key><string>%d</string>
	<key>LSMinimumSystemVersion</key><string>14.0</string>
	<key>LSUIElement</key><true/>
	<key>LoaferBinary</key><string>%s</string>
</dict>
</plist>
`
