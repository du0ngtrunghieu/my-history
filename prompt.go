package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

func (a *App) prompt(label, def string) (string, error) {
	if def != "" {
		fmt.Fprintf(a.Err, "%s [%s]: ", label, def)
	} else {
		fmt.Fprintf(a.Err, "%s: ", label)
	}
	value, err := readLine(a.reader())
	if err == io.EOF {
		return "", errors.New("thiếu dữ liệu nhập")
	}
	if err != nil {
		return "", err
	}
	if value == "" {
		return def, nil
	}
	return value, nil
}

func (a *App) confirm(question string) (bool, error) {
	fmt.Fprintf(a.Err, "%s? [y/N]: ", question)
	value, err := readLine(a.reader())
	if err == io.EOF {
		return false, errors.New("thiếu dữ liệu nhập")
	}
	if err != nil {
		return false, err
	}
	switch strings.ToLower(value) {
	case "y", "yes", "c", "co", "có":
		return true, nil
	default:
		return false, nil
	}
}

func (a *App) readSecret(prompt string) (string, error) {
	fmt.Fprint(a.Err, prompt)
	defer fmt.Fprintln(a.Err)
	if !a.termSet && (a.In == nil || a.In == os.Stdin) && term.IsTerminal(int(os.Stdin.Fd())) {
		secret, err := term.ReadPassword(int(os.Stdin.Fd()))
		if err != nil {
			return "", err
		}
		return cleanToken(string(secret)), nil
	}
	line, err := readLine(a.reader())
	if err == io.EOF {
		return "", errors.New("thiếu token")
	}
	if err != nil {
		return "", err
	}
	return cleanToken(line), nil
}

func (a *App) interactive() bool {
	if a.termSet {
		return a.StdinTerm
	}
	return term.IsTerminal(int(os.Stdin.Fd()))
}

func (a *App) reader() io.Reader {
	if a.In == nil {
		return os.Stdin
	}
	return a.In
}

func readLine(r io.Reader) (string, error) {
	var buf bytes.Buffer
	one := make([]byte, 1)
	for {
		n, err := r.Read(one)
		if n == 1 {
			if one[0] == '\n' {
				break
			}
			if one[0] != '\r' {
				buf.WriteByte(one[0])
			}
			continue
		}
		if err == io.EOF {
			if buf.Len() == 0 {
				return "", io.EOF
			}
			return strings.TrimSpace(buf.String()), nil
		}
		if err != nil {
			return "", err
		}
	}
	return strings.TrimSpace(buf.String()), nil
}

func cleanToken(s string) string {
	return trimWrapping(s)
}
