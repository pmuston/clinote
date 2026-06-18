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
	// Subcommand dispatch: `clinote new <path>` scaffolds a new notebook.
	if len(os.Args) >= 2 && os.Args[1] == "new" {
		if err := newCmd(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "clinote:", err)
			os.Exit(1)
		}
		return
	}

	flag.Usage = usage
	noBrowser := flag.Bool("no-browser", false, "Do not open the browser")
	flag.Parse()

	if err := run(flag.Arg(0), *noBrowser); err != nil {
		fmt.Fprintln(os.Stderr, "clinote:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "Usage:")
	fmt.Fprintln(os.Stderr, "  clinote [flags] [path/to/notebook.md]   open a notebook")
	fmt.Fprintln(os.Stderr, "  clinote new [flags] <path>              create a notebook and open it")
	fmt.Fprintln(os.Stderr, "  clinote                                 list .md files in cwd")
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "Flags:")
	flag.PrintDefaults()
}

func run(path string, noBrowser bool) error {
	if path == "" {
		return pickerLoop()
	}
	return serve(path, noBrowser)
}

// newCmd handles the `clinote new <path>` subcommand: writes a starter
// notebook to path (refusing to overwrite), then opens it just like
// `clinote <path>` would.
func newCmd(args []string) error {
	fs := flag.NewFlagSet("new", flag.ExitOnError)
	noBrowser := fs.Bool("no-browser", false, "Do not open the browser")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: clinote new [flags] <path>")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		fs.Usage()
		return errors.New("expected exactly one path")
	}
	path := fs.Arg(0)
	if err := createNotebook(path); err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "created", path)
	return serve(path, *noBrowser)
}

// createNotebook writes a starter notebook to path. The file is refused if it
// already exists. Title is derived from the filename.
func createNotebook(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if _, err := os.Stat(abs); err == nil {
		return fmt.Errorf("%s already exists; refusing to overwrite", path)
	} else if !os.IsNotExist(err) {
		return err
	}
	title := deriveTitle(abs)
	body := starterNotebook(title, time.Now().UTC().Format(time.RFC3339))
	if err := os.WriteFile(abs, []byte(body), 0o644); err != nil {
		return fmt.Errorf("write notebook: %w", err)
	}
	return nil
}

func deriveTitle(path string) string {
	name := filepath.Base(path)
	name = strings.TrimSuffix(name, ".md")
	name = strings.ReplaceAll(name, "-", " ")
	name = strings.ReplaceAll(name, "_", " ")
	if name == "" {
		return "Notebook"
	}
	return strings.ToUpper(name[:1]) + name[1:]
}

// starterNotebook returns the template that `clinote new` writes. One example
// command cell so the first thing a user does is click Run and see it work.
//
// Defaults `editable: true` and `width: full` because a notebook you're just
// creating is almost certainly one you want to author freely (edit cells,
// see wide output). Delete those lines from the YAML if you want to lock the
// notebook down later.
func starterNotebook(title, created string) string {
	return "---\n" +
		"title: " + title + "\n" +
		"created: " + created + "\n" +
		"shell: bash\n" +
		"editable: true\n" +
		"width: full\n" +
		"---\n" +
		"\n" +
		"# " + title + "\n" +
		"\n" +
		"```sh\n" +
		"echo \"hello from clinote\"\n" +
		"```\n"
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
