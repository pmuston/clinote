package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/pmuston/clinote/internal/notebook"
	"github.com/pmuston/clinote/internal/runner"
	"github.com/pmuston/clinote/internal/server"
)

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: clinote [path/to/notebook.md]\n\n")
		flag.PrintDefaults()
	}
	noBrowser := flag.Bool("no-browser", false, "Do not open the browser")
	flag.Parse()

	if err := run(flag.Arg(0), *noBrowser); err != nil {
		fmt.Fprintln(os.Stderr, "clinote:", err)
		os.Exit(1)
	}
}

func run(path string, noBrowser bool) error {
	if path == "" {
		return pickerLoop()
	}
	return serve(path, noBrowser)
}

func serve(path string, noBrowser bool) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	f, err := os.Open(abs)
	if err != nil {
		return fmt.Errorf("open notebook: %w", err)
	}
	nb, err := notebook.Parse(f)
	f.Close()
	if err != nil {
		return fmt.Errorf("parse notebook: %w", err)
	}

	shell := nb.FrontMatter.Shell
	if shell == "" {
		shell = "bash"
	}
	r, err := runner.New(shell)
	if err != nil {
		return fmt.Errorf("start runner: %w", err)
	}
	defer r.Close()

	srv, err := server.New(abs, nb, r)
	if err != nil {
		return err
	}

	e := echo.New()
	e.HideBanner = true
	e.HidePort = true
	e.Use(middleware.Recover())
	srv.Register(e)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	url := "http://" + ln.Addr().String()
	fmt.Println(url)

	go func() {
		e.Listener = ln
		if err := e.Start(""); err != nil && !errors.Is(err, http.ErrServerClosed) {
			fmt.Fprintln(os.Stderr, "server error:", err)
		}
	}()

	if !noBrowser && browserAllowed() {
		go func() {
			time.Sleep(150 * time.Millisecond)
			openBrowser(url)
		}()
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return e.Shutdown(ctx)
}

// browserAllowed checks the BROWSER env var. If it's set to a value indicating
// "no browser" (empty, "none"), we skip the open. Otherwise we honour the
// system default. (§3.2.)
func browserAllowed() bool {
	v, set := os.LookupEnv("BROWSER")
	if !set {
		return true
	}
	v = strings.TrimSpace(strings.ToLower(v))
	switch v {
	case "", "none", "false", "0":
		return false
	}
	return true
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "linux":
		cmd = exec.Command("xdg-open", url)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", url)
	}
	if cmd != nil {
		_ = cmd.Start()
	}
}

// pickerLoop runs a minimal picker server when invoked without a path.
// It serves a list of .md files in the cwd; the user picks one, the server
// restarts with that path. For v1 we simplify: print the list and prompt to
// re-invoke with a path.
func pickerLoop() error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(cwd)
	if err != nil {
		return err
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			files = append(files, e.Name())
		}
	}
	if len(files) == 0 {
		return fmt.Errorf("no .md files in %s; pass a path explicitly", cwd)
	}
	fmt.Println("Notebooks in", cwd+":")
	for _, f := range files {
		fmt.Println("  ", f)
	}
	fmt.Println()
	fmt.Println("Run: clinote <filename>")
	return nil
}
