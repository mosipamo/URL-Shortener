// Package envfile loads KEY=VALUE settings from a .env file into the process
// environment. It is deliberately tiny (standard library only).
//
// Rules:
//   - the file must be UTF-8 (a leading byte order mark is accepted)
//   - blank lines and lines starting with # are ignored
//   - an optional "export " prefix is allowed
//   - the line is split at the FIRST '='; key and value are trimmed
//   - a value wrapped in matching single or double quotes has them removed
//   - no inline comments and no escape sequences: '#' and '\' are ordinary
//     characters, because they legitimately appear in passwords and URLs
package envfile

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"
)

// Parse reads KEY=VALUE pairs from r. Error messages contain the line number
// but never the line itself, because it may hold a secret.
func Parse(r io.Reader) (map[string]string, error) {
	out := make(map[string]string)
	sc := bufio.NewScanner(r)
	for n := 1; sc.Scan(); n++ {
		raw := sc.Text()
		if n == 1 {
			raw = strings.TrimPrefix(raw, "\uFEFF") // UTF-8 byte order mark, added by some Windows editors
		}
		if strings.ContainsRune(raw, 0) {
			// UTF-16 text (e.g. PowerShell's `>` redirection) is full of NUL bytes.
			return nil, fmt.Errorf("line %d: not UTF-8 text; save the file as UTF-8", n)
		}
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, value, ok := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		if !ok || key == "" || strings.ContainsAny(key, " \t") {
			return nil, fmt.Errorf("line %d: expected KEY=VALUE", n)
		}
		out[key] = unquote(strings.TrimSpace(value))
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func unquote(v string) string {
	if len(v) >= 2 {
		if first, last := v[0], v[len(v)-1]; first == last && (first == '"' || first == '\'') {
			return v[1 : len(v)-1]
		}
	}
	return v
}

// Load reads the file at path and sets every variable that is NOT already
// set in the environment, so real environment variables (and anything exported
// in the shell) win over the file. A missing file is not an error: .env is
// optional.
func Load(path string) error {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	vars, err := Parse(f)
	if err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	for k, v := range vars {
		if _, set := os.LookupEnv(k); set {
			continue
		}
		if err := os.Setenv(k, v); err != nil {
			return fmt.Errorf("set %s: %w", k, err)
		}
	}
	return nil
}
