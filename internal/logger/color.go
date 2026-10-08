package logger

import (
	"hash/fnv"
	"io"
	"os"
)

// plainSyncer writes log lines and never fsyncs the destination.
// fsync on a terminal or a pipe returns EINVAL: "sync /dev/stdout: invalid argument".
type plainSyncer struct {
	// Writer receives the encoded line.
	io.Writer
}

const (
	// timeLayout is the local wall clock printed in every log line.
	timeLayout = "2006-01-02 15:04:05"
	// fieldRepo is the context key printed as the repository name.
	fieldRepo = "repo"
	// ansiReset returns the terminal to its default color.
	ansiReset = "\033[0m"
	// ansiTime is #C5CDD8, a cool gray for the clock. Contrast is about 13:1 on black
	// and about 11:1 on Ubuntu aubergine #300A24.
	ansiTime = "\033[38;2;197;205;216m"
	// ansiElapsed is #7FD1C7, a muted teal for time already spent.
	ansiElapsed = "\033[38;2;127;209;199m"
	// ansiLeft is #E6C88A, a muted gold for the estimated remainder.
	ansiLeft = "\033[38;2;230;200;138m"
	// ansiPercent is #C9B8F0, a muted lilac for the completion share.
	ansiPercent = "\033[38;2;201;184;240m"
	// levelWidth pads short level names so the following columns line up.
	levelWidth = 5
)

// Write copies one log entry.
func (w *plainSyncer) Write(p []byte) (int, error) {
	return w.Writer.Write(p)
}

// Sync does not call fsync. Log lines are already written before Sync runs.
func (w *plainSyncer) Sync() error {
	return nil
}

// colorEnabled reports whether ANSI color is appropriate for this writer.
// A set NO_COLOR variable disables color even when it is empty.
func colorEnabled(w io.Writer) bool {
	if !envAllowsColor(os.LookupEnv, os.Getenv) {
		return false
	}

	f, ok := w.(*os.File)
	if !ok {
		return false
	}

	info, err := f.Stat()
	if err != nil {
		return false
	}

	return info.Mode()&os.ModeCharDevice != 0
}

// envAllowsColor honors NO_COLOR and TERM=dumb.
func envAllowsColor(lookup func(string) (string, bool), getenv func(string) string) bool {
	if _, disabled := lookup("NO_COLOR"); disabled || getenv("TERM") == "dumb" {
		return false
	}

	return true
}

// levelStyle is the color of the level word.
func (c *textCore) levelStyle(level string) string {
	switch level {
	case "debug":
		return "\033[90m"
	case "info":
		return "\033[1;34m"
	case "warn":
		return "\033[1;33m"
	case "error", "dpanic", "panic", "fatal":
		return "\033[1;31m"
	default:
		return ""
	}
}

// valueStyle paints pace numbers. Other fields stay in the default terminal color.
func (c *textCore) valueStyle(key string) string {
	switch key {
	case "elapsed":
		return ansiElapsed
	case "left":
		return ansiLeft
	case "percent":
		return ansiPercent
	default:
		return ""
	}
}

// repoColor is stable for one repository name and avoids red and yellow.
// Those two colors are reserved for errors and warnings.
func (c *textCore) repoColor(name string) string {
	palette := [...]string{
		"\033[1;36m",
		"\033[1;32m",
		"\033[1;35m",
		"\033[1;96m",
		"\033[1;92m",
		"\033[1;95m",
	}

	h := fnv.New32a()

	_, err := h.Write([]byte(name))
	if err != nil {
		return palette[0]
	}

	return palette[h.Sum32()%uint32(len(palette))]
}
