package logger

import (
	"io"
	"os"
	"strings"
	"sync"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var (
	// global is the package-wide sugared logger used by helper functions.
	//
	//nolint:gochecknoglobals // Logger is used all over the project, so it's okay.
	global *zap.SugaredLogger
	// defaultLevel stores the atomic log level for the global logger.
	//
	//nolint:gochecknoglobals // If the logging level is not set, the application will have no logs.
	defaultLevel = zap.NewAtomicLevelAt(zap.InfoLevel)
	// fatalHandler is an optional test override for fatal exit behavior.
	//
	//nolint:gochecknoglobals // Test helper to override fatal behavior.
	fatalHandler func(int)
	// fatalHandlerMutex protects fatalHandler reads and writes.
	//
	//nolint:gochecknoglobals // Should be able to override fatal handler in tests.
	fatalHandlerMutex sync.Mutex
)

// init initializes the default global logger at info level.
func init() { //nolint:gochecknoinits // If the logging level is not set, the application will have no logs.
	SetLogger(New(defaultLevel))
}

// New creates a new instance of *zap.SugaredLogger with text lines on stdout.
// If the logging level is not provided, the default level (zap.InfoLevel) will be used.
func New(level zapcore.LevelEnabler, options ...zap.Option) *zap.SugaredLogger {
	return NewWithWriter(level, os.Stdout, options...)
}

// NewWithWriter keeps logs testable and safe for concurrent writers.
// A terminal colors the level and the repository name. A pipe, a file, or NO_COLOR stays plain text.
func NewWithWriter(level zapcore.LevelEnabler, out io.Writer, options ...zap.Option) *zap.SugaredLogger {
	if level == nil {
		level = defaultLevel
	}

	sink := &plainSyncer{
		Writer: out,
	}

	core := newTextCore(level, sink, colorEnabled(out))

	return zap.New(core, options...).Sugar()
}

// ParseLogLevel converts string input to zap log level.
func ParseLogLevel(s string) (zapcore.Level, bool) {
	normalized := strings.ToLower(strings.TrimSpace(s))
	if normalized == "" {
		return zapcore.InfoLevel, false
	}

	level, err := zapcore.ParseLevel(normalized)
	if err != nil {
		return zapcore.InfoLevel, false
	}

	return level, true
}

// Level returns the current logging level of the global logger.
func Level() zapcore.Level {
	return defaultLevel.Level()
}

// IsDebugLevel returns true if the current logging level is debug.
func IsDebugLevel() bool {
	return Level() == zapcore.DebugLevel
}

// Logger returns the global logger.
func Logger() *zap.SugaredLogger {
	return global
}

// SetLogger sets the global logger.
// This function is not thread-safe.
func SetLogger(l *zap.SugaredLogger) {
	global = l
}

// SetFatalHandler overrides fatal behavior for tests.
func SetFatalHandler(handler func(int)) {
	fatalHandlerMutex.Lock()
	defer fatalHandlerMutex.Unlock()

	fatalHandler = handler
}

// SetLevel sets the log level for the global logger.
func SetLevel(level zapcore.Level) {
	//nolint:errcheck // No need to check the error here.
	defer global.Sync()

	defaultLevel.SetLevel(level)
}
