package logger

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap/zapcore"
)

// TestNew tests the New function.
func TestNew(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		level zapcore.LevelEnabler
	}{
		{
			name:  "with debug level",
			level: zapcore.DebugLevel,
		},
		{
			name:  "with info level",
			level: zapcore.InfoLevel,
		},
		{
			name:  "with error level",
			level: zapcore.ErrorLevel,
		},
		{
			name:  "with nil level",
			level: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			logger := New(tt.level)
			assert.NotNil(t, logger)
		})
	}
}

// TestParseLogLevel tests the ParseLogLevel function.
func TestParseLogLevel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected zapcore.Level
		valid    bool
	}{
		{
			name:     "debug level",
			input:    "debug",
			expected: zapcore.DebugLevel,
			valid:    true,
		},
		{
			name:     "info level",
			input:    "info",
			expected: zapcore.InfoLevel,
			valid:    true,
		},
		{
			name:     "warn level",
			input:    "warn",
			expected: zapcore.WarnLevel,
			valid:    true,
		},
		{
			name:     "error level",
			input:    "error",
			expected: zapcore.ErrorLevel,
			valid:    true,
		},
		{
			name:     "dpanic level",
			input:    "dpanic",
			expected: zapcore.DPanicLevel,
			valid:    true,
		},
		{
			name:     "panic level",
			input:    "panic",
			expected: zapcore.PanicLevel,
			valid:    true,
		},
		{
			name:     "fatal level",
			input:    "fatal",
			expected: zapcore.FatalLevel,
			valid:    true,
		},
		{
			name:     "uppercase debug",
			input:    "DEBUG",
			expected: zapcore.DebugLevel,
			valid:    true,
		},
		{
			name:     "mixed case info",
			input:    "Info",
			expected: zapcore.InfoLevel,
			valid:    true,
		},
		{
			name:     "with spaces",
			input:    " debug ",
			expected: zapcore.DebugLevel,
			valid:    true,
		},
		{
			name:     "invalid level",
			input:    "invalid",
			expected: zapcore.InfoLevel,
			valid:    false,
		},
		{
			name:     "empty string",
			input:    "",
			expected: zapcore.InfoLevel,
			valid:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			level, valid := ParseLogLevel(tt.input)
			assert.Equal(t, tt.expected, level)
			assert.Equal(t, tt.valid, valid)
		})
	}
}

// TestLevel tests the Level function.
func TestLevel(t *testing.T) {
	t.Parallel()

	level := Level()
	assert.NotNil(t, level)
}

// TestLogger tests the Logger function.
func TestLogger(t *testing.T) {
	t.Parallel()

	logger := Logger()
	assert.NotNil(t, logger)
}

// TestSetLogger tests the SetLogger function.
func TestSetLogger(t *testing.T) {
	// Don't run in parallel to avoid race conditions with global logger state.
	originalLogger := Logger()
	defer SetLogger(originalLogger) // Restore original logger

	newLogger := New(zapcore.DebugLevel)
	SetLogger(newLogger)

	currentLogger := Logger()
	assert.Equal(t, newLogger, currentLogger)
}

// TestSetLevel tests the SetLevel function.
func TestSetLevel(t *testing.T) {
	// Don't run in parallel to avoid race conditions with global logger state.
	originalLevel := Level()
	defer SetLevel(originalLevel) // Restore original level

	SetLevel(zapcore.DebugLevel)

	level := Level()
	assert.Equal(t, zapcore.DebugLevel, level)

	SetLevel(zapcore.ErrorLevel)

	level = Level()
	assert.Equal(t, zapcore.ErrorLevel, level)
}
