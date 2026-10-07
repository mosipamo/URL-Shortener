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

func Parse(r io.Reader) (map[string]string, error) {
	out := make(map[string]string)
	sc := bufio.NewScanner(r)
	for n := 1; sc.Scan(); n++ {
		raw := sc.Text()
		if n == 1 {
			raw = strings.TrimPrefix(raw, "\uFEFF")
		}
		if strings.ContainsRune(raw, 0) {
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
