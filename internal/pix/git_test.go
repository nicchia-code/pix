package pix

import (
	"strings"
	"testing"
)

func TestMakeRepoID(t *testing.T) {
	id, err := makeRepoID("/tmp/My App")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(id, "my-app-") {
		t.Fatalf("repo id = %q", id)
	}
}

func TestShellQuote(t *testing.T) {
	got := shellQuote("a'b")
	want := `'a'"'"'b'`
	if got != want {
		t.Fatalf("quote = %q, want %q", got, want)
	}
}
