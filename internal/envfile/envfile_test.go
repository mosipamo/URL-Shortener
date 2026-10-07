package envfile

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	in := `
# a comment
export ADDR=:8080
DATABASE_URL=postgres://u:p#ss=1@localhost:5432/db?sslmode=disable
QUOTED_D="hello world"
QUOTED_S='single # kept'
EMPTY=
  SPACED  =  value with spaces
MISMATCH="open
`
	got, err := Parse(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"ADDR":         ":8080",
		"DATABASE_URL": "postgres://u:p#ss=1@localhost:5432/db?sslmode=disable",
		"QUOTED_D":     "hello world",
		"QUOTED_S":     "single # kept",
		"EMPTY":        "",
		"SPACED":       "value with spaces",
		"MISMATCH":     `"open`,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %#v\nwant %#v", got, want)
	}
}

func TestParseUTF8BOMAndWindowsLineEndings(t *testing.T) {
	got, err := Parse(strings.NewReader("\uFEFFDATABASE_URL=postgres://u:p@h/db\r\nADDR=:8080\r\n"))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"DATABASE_URL": "postgres://u:p@h/db", "ADDR": ":8080"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestParseRejectsUTF16(t *testing.T) {
	utf16 := "\xff\xfeA\x00D\x00D\x00R\x00=\x00p\x00"
	_, err := Parse(strings.NewReader(utf16))
	if err == nil || !strings.Contains(err.Error(), "UTF-8") || !strings.Contains(err.Error(), "line 1") {
		t.Fatalf("err = %v, want a clear 'save as UTF-8' error", err)
	}
}

func TestParseErrorsDoNotLeakValues(t *testing.T) {
	for _, in := range []string{"just-a-word-with-secret", "=novalue-secret", "BAD KEY=secret"} {
		_, err := Parse(strings.NewReader(in))
		if err == nil {
			t.Fatalf("Parse(%q) should fail", in)
		}
		if !strings.Contains(err.Error(), "line 1") || strings.Contains(err.Error(), "secret") {
			t.Errorf("Parse(%q) error = %q", in, err)
		}
	}
}

func TestLoad(t *testing.T) {
	t.Setenv("ENVFILE_TEST_NEW", "")
	os.Unsetenv("ENVFILE_TEST_NEW")
	t.Setenv("ENVFILE_TEST_EXISTING", "from-real-env")

	path := filepath.Join(t.TempDir(), ".env")
	content := "ENVFILE_TEST_NEW=from-file\nENVFILE_TEST_EXISTING=from-file\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Load(path); err != nil {
		t.Fatal(err)
	}
	if got := os.Getenv("ENVFILE_TEST_NEW"); got != "from-file" {
		t.Errorf("new variable = %q, want from-file", got)
	}
	if got := os.Getenv("ENVFILE_TEST_EXISTING"); got != "from-real-env" {
		t.Errorf("existing variable = %q: the real environment must win over the file", got)
	}
}

func TestLoadMissingFileIsNotAnError(t *testing.T) {
	if err := Load(filepath.Join(t.TempDir(), "nope.env")); err != nil {
		t.Fatalf("missing file should be ignored, got %v", err)
	}
}

func TestLoadInvalidFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte("this is not valid\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Load(path); err == nil || !strings.Contains(err.Error(), "line 1") {
		t.Fatalf("err = %v, want a line-1 parse error", err)
	}
}

func TestLoadUnreadablePath(t *testing.T) {
	if err := Load(t.TempDir()); err == nil {
		t.Fatal("expected an error when the path is a directory")
	}
}
