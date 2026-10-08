package obs

import (
	"archive/zip"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/pprof"
	"os"
	"path/filepath"
	"runtime"
	rpprof "runtime/pprof"
	"runtime/trace"
	"strings"
	"sync/atomic"
	"time"
)

// Serve runs net/http/pprof on addr (keep it on localhost) until the
// process exits.
func Serve(addr string) {
	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	go func() {
		slog.Info("pprof", "addr", addr)
		if err := http.ListenAndServe(addr, mux); err != nil {
			slog.Error("pprof", "err", err)
		}
	}()
}

var dumping atomic.Bool

// Dump writes a 5 s runtime trace, then a 30 s CPU profile, then heap and
// goroutine profiles, into a new folder under Dir()/prof, in the
// background. It returns the folder, or "" if a dump is already running.
// The heap profile is only useful with LOAFER_MEMPROFILE=1 set at start.
func Dump() string {
	if !dumping.CompareAndSwap(false, true) {
		return ""
	}
	dir := filepath.Join(Dir(), "prof", time.Now().Format("2006-01-02T15-04-05"))
	go func() {
		defer dumping.Store(false)
		if err := dump(dir); err != nil {
			slog.Error("profile dump", "dir", dir, "err", err)
			return
		}
		slog.Info("profile dump", "dir", dir)
	}()
	return dir
}

func dump(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	write := func(name string, f func(io.Writer) error) error {
		out, err := os.Create(filepath.Join(dir, name))
		if err != nil {
			return err
		}
		defer out.Close()
		return f(out)
	}
	if err := write("trace.out", func(w io.Writer) error {
		if err := trace.Start(w); err != nil {
			return err
		}
		time.Sleep(5 * time.Second)
		trace.Stop()
		return nil
	}); err != nil {
		return err
	}
	if err := write("cpu.pprof", func(w io.Writer) error {
		if err := rpprof.StartCPUProfile(w); err != nil {
			return err
		}
		time.Sleep(30 * time.Second)
		rpprof.StopCPUProfile()
		return nil
	}); err != nil {
		return err
	}
	for _, p := range []string{"heap", "goroutine"} {
		if err := write(p+".pprof", func(w io.Writer) error { return rpprof.Lookup(p).WriteTo(w, 0) }); err != nil {
			return err
		}
	}
	return nil
}

// Report zips what's useful for a bug into w: the logs and the most recent
// profile dump under Dir(), plus versions. Every log line was redacted as
// it was written; text files are redacted again on the way in, in case.
func Report(w io.Writer, version string) error {
	z := zip.NewWriter(w)
	info, _ := z.Create("info.txt")
	fmt.Fprintf(info, "loafer %s\n%s %s/%s\nTERM=%s TERM_PROGRAM=%s\n",
		version, runtime.Version(), runtime.GOOS, runtime.GOARCH, os.Getenv("TERM"), os.Getenv("TERM_PROGRAM"))

	root := Dir()
	latest := ""
	if ds, _ := os.ReadDir(filepath.Join(root, "prof")); len(ds) > 0 {
		latest = filepath.Join(root, "prof", ds[len(ds)-1].Name())
	}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if strings.Contains(p, string(filepath.Separator)+"prof"+string(filepath.Separator)) && !strings.HasPrefix(p, latest+string(filepath.Separator)) {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if !strings.HasSuffix(p, ".pprof") && !strings.HasSuffix(p, ".out") {
			b = []byte(Redact(string(b)))
		}
		f, err := z.Create(rel)
		if err != nil {
			return err
		}
		_, err = f.Write(b)
		return err
	})
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return z.Close()
}
