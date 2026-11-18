// Command glops is a small GitLab terminal client for macOS and Linux.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

var version = "dev"

const usageText = `glops — GitLab trên terminal

Lưu endpoint, username và token một lần, rồi dùng lại cho các lệnh sau.
Chạy được trên macOS và Linux.

Cách dùng:
  glops setup
  glops merge <link-merge-request>
  glops tag [--name <tag> --ref <nhánh-hoặc-tag>] [--project <project>] [--yes]
  glops retry <link-pipeline>
  glops whoami
  glops config

Lệnh:
  setup     Lưu endpoint, username và personal access token
  merge     Merge merge request nếu tài khoản có quyền
  tag       Tạo tag từ một nhánh hoặc tag để chạy pipeline
  retry     Chạy lại pipeline
  whoami    Kiểm tra token bằng API
  config    Xem cấu hình đã lưu (token được che)
  version   In phiên bản

Token cần scope api. File cấu hình chỉ chủ tài khoản đọc được:
  macOS: ~/Library/Application Support/glops/config.json
  Linux: ~/.config/glops/config.json

Ví dụ:
  glops setup --endpoint https://gitlab.company.com --username hieu
  glops merge https://gitlab.company.com/team/app/-/merge_requests/42
  glops tag --name v1.2.3 --ref main --yes
  glops retry https://gitlab.company.com/team/app/-/pipelines/900
`

type App struct {
	In        io.Reader
	Out       io.Writer
	Err       io.Writer
	ConfigDir string
	StdinTerm bool
	termSet   bool
	// Sleep waits between status checks. Nil uses the default delay.
	Sleep      func()
	LookRemote func(endpoint string) (string, error)
}

func newApp() *App {
	return &App{In: os.Stdin, Out: os.Stdout, Err: os.Stderr}
}

func main() {
	app := newApp()
	if err := app.Run(os.Args[1:]); err != nil {
		if !errors.Is(err, errHelp) {
			if msg := err.Error(); msg != "" {
				fmt.Fprintf(os.Stderr, "lỗi: %s\n", msg)
			}
		}
		os.Exit(exitCode(err))
	}
}

func (a *App) Run(args []string) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		fmt.Fprint(a.Out, usageText)
		return nil
	}
	switch args[0] {
	case "version", "--version":
		fmt.Fprintf(a.Out, "glops %s\n", version)
		return nil
	case "setup":
		return a.cmdSetup(args[1:])
	case "config":
		return a.cmdConfig(args[1:])
	case "merge":
		return a.cmdMerge(args[1:])
	case "tag":
		return a.cmdTag(args[1:])
	case "retry":
		return a.cmdRetry(args[1:])
	case "whoami":
		return a.cmdWhoami(args[1:])
	default:
		fmt.Fprint(a.Err, usageText)
		return code(2, fmt.Errorf("không rõ lệnh %q", args[0]))
	}
}

type codedError struct {
	code int
	err  error
}

func (e *codedError) Error() string {
	if e.err == nil {
		return ""
	}
	return e.err.Error()
}

func (e *codedError) Unwrap() error { return e.err }

func code(n int, err error) error {
	if err == nil {
		return nil
	}
	return &codedError{code: n, err: err}
}

var errHelp = errors.New("help")

type silentError struct{}

func (silentError) Error() string { return "" }

func flagParse(fs *flag.FlagSet, args []string) error {
	for _, arg := range args {
		if arg == "-h" || arg == "--help" {
			fs.Usage()
			return errHelp
		}
	}
	ordered, err := reorderFlags(fs, args)
	if err != nil {
		fmt.Fprintln(fs.Output(), err.Error())
		fs.Usage()
		return code(2, silentError{})
	}
	if err := fs.Parse(ordered); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return errHelp
		}
		return code(2, silentError{})
	}
	return nil
}

// reorderFlags lets flags follow positional arguments, which the standard
// flag parser otherwise stops reading.
func reorderFlags(fs *flag.FlagSet, args []string) ([]string, error) {
	flags := make([]string, 0, len(args))
	positional := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			positional = append(positional, args[i+1:]...)
			break
		}
		if arg == "-" || !strings.HasPrefix(arg, "-") {
			positional = append(positional, arg)
			continue
		}
		name := strings.TrimLeft(arg, "-")
		attached := false
		if eq := strings.IndexByte(name, '='); eq >= 0 {
			attached = true
			name = name[:eq]
		}
		flags = append(flags, arg)
		known := fs.Lookup(name)
		if known == nil || attached || isBoolFlag(known) {
			continue
		}
		if i+1 >= len(args) {
			return nil, fmt.Errorf("thiếu giá trị cho --%s", name)
		}
		i++
		flags = append(flags, args[i])
	}
	return append(flags, positional...), nil
}

func isBoolFlag(f *flag.Flag) bool {
	type boolFlag interface{ IsBoolFlag() bool }
	bf, ok := f.Value.(boolFlag)
	return ok && bf.IsBoolFlag()
}

func exitCode(err error) int {
	if errors.Is(err, errHelp) {
		return 0
	}
	var c *codedError
	if errors.As(err, &c) && c.code != 0 {
		return c.code
	}
	return 1
}

func defaultSleep() {
	time.Sleep(700 * time.Millisecond)
}

func (a *App) sleep() {
	if a.Sleep != nil {
		a.Sleep()
		return
	}
	defaultSleep()
}
