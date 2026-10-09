package logger

import (
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/assert"
)

// TestContextLoggingFunctions tests all the context-based logging functions.
func TestContextLoggingFunctions(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	// Test Debug functions.
	Debug(ctx, "test debug message")
	Debugf(ctx, "test debug message: %s", "formatted")
	DebugKV(ctx, "test debug message", "key", "value")

	// Test Info functions.
	Info(ctx, "test info message")
	Infof(ctx, "test info message: %s", "formatted")
	InfoKV(ctx, "test info message", "key", "value")

	// Test Warn functions.
	Warn(ctx, "test warn message")
	Warnf(ctx, "test warn message: %s", "formatted")
	WarnKV(ctx, "test warn message", "key", "value")

	// Test Error functions.
	Error(ctx, "test error message")
	Errorf(ctx, "test error message: %s", "formatted")
	ErrorKV(ctx, "test error message", "key", "value")

	// These should not panic in tests,
	// but we cannot easily test Fatal and Panic functions
	// without causing the test to exit or panic.
}

// TestContextLoggingWithValidContext tests logging with valid context.
func TestContextLoggingWithValidContext(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	// These should not panic with valid context.
	Debug(ctx, "test message")
	Info(ctx, "test message")
	Warn(ctx, "test message")
	Error(ctx, "test message")
}

// TestLoggerInitialization tests that the logger is properly initialized.
func TestLoggerInitialization(t *testing.T) {
	t.Parallel()

	// The logger should be initialized in the init function.
	logger := Logger()
	assert.NotNil(t, logger)

	// The default level should be set.
	level := Level()
	assert.NotNil(t, level)
}

// TestLoggerThreadSafety tests basic thread safety of logger operations.
func TestLoggerThreadSafety(t *testing.T) {
	// Don't run in parallel to avoid race conditions with global logger state.
	synctest.Test(t, func(t *testing.T) {
		ctx := t.Context()

		for range 10 {
			go func() {
				Info(ctx, "concurrent message")
			}()
		}

		synctest.Wait()
	})
}
