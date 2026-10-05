package cmd

import (
	"bytes"
	"strings"
	"testing"
)

func TestHelpVersionAndInvalidFlags(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"version"}, {"--version"}} {
		var out, err bytes.Buffer

		if code := Execute(args, &out, &err); code != exitOK {
			t.Fatal(args, code, err.String())
		}
	}

	var out, err bytes.Buffer

	if code := Execute([]string{"--jobs=0"}, &out, &err); code != exitUsage || !strings.Contains(err.String(), "jobs") {
		t.Fatal(code, err.String())
	}
}

func TestCobraHelpVersionAndCompletionIgnoreInvalidEnvironment(t *testing.T) {
	t.Setenv("JOBS", "invalid")

	commands := [][]string{
		{"--help"},
		{"help"},
		{"version"},
		{"--version"},
		{"completion", "bash"},
	}

	for _, args := range commands {
		var out, err bytes.Buffer

		if code := Execute(args, &out, &err); code != exitOK || out.Len() == 0 {
			t.Fatalf("%v: code=%d out=%s err=%s", args, code, out.String(), err.String())
		}
	}
}

func TestCobraFlagsOverrideEnvironmentAndCommandsDoNotShareState(t *testing.T) {
	t.Setenv("BASE_DIR", t.TempDir())
	t.Setenv("JOBS", "9")

	for _, args := range [][]string{{"-n", "-j", "2"}, {"--dry-run"}} {
		var out, err bytes.Buffer

		if code := Execute(args, &out, &err); code != exitOK {
			t.Fatal(code, err.String())
		}

		want := "workers=9"
		if len(args) > 1 {
			want = "workers=2"
		}

		if !strings.Contains(out.String(), want) {
			t.Fatalf("want %s in %s", want, out.String())
		}
	}
}

func TestCobraRejectsInvalidArguments(t *testing.T) {
	commands := [][]string{
		{"--unknown"},
		{"--jobs", "oops"},
		{"--attempts", "0"},
		{"unexpected"},
		{"version", "unexpected"},
	}

	for _, args := range commands {
		var out, err bytes.Buffer

		if code := Execute(args, &out, &err); code != exitUsage {
			t.Fatalf("%v: %d %s", args, code, err.String())
		}
	}
}
