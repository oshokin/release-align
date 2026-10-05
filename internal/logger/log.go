package logger

import (
	"context"

	"go.uber.org/zap"
)

// Debug writes a debug level message using the logger from the context.
func Debug(ctx context.Context, args ...any) {
	FromContext(ctx).Debug(args...)
}

// Debugf writes a formatted debug level message using the logger from the context.
func Debugf(ctx context.Context, format string, args ...any) {
	FromContext(ctx).Debugf(format, args...)
}

// DebugKV writes a message and key-value pairs
// at the debug level using the logger from the context.
func DebugKV(ctx context.Context, message string, kvs ...any) {
	FromContext(ctx).Debugw(message, kvs...)
}

// Info writes an information level message using the logger from the context.
func Info(ctx context.Context, args ...any) {
	FromContext(ctx).Info(args...)
}

// Infof writes a formatted information level message using the logger from the context.
func Infof(ctx context.Context, format string, args ...any) {
	FromContext(ctx).Infof(format, args...)
}

// InfoKV writes a message and key-value pairs
// at the information level using the logger from the context.
func InfoKV(ctx context.Context, message string, kvs ...any) {
	FromContext(ctx).Infow(message, kvs...)
}

// Warn writes a warning level message using the logger from the context.
func Warn(ctx context.Context, args ...any) {
	FromContext(ctx).Warn(args...)
}

// Warnf writes a formatted warning level message using the logger from the context.
func Warnf(ctx context.Context, format string, args ...any) {
	FromContext(ctx).Warnf(format, args...)
}

// WarnKV writes a message and key-value pairs
// at the warning level using the logger from the context.
func WarnKV(ctx context.Context, message string, kvs ...any) {
	FromContext(ctx).Warnw(message, kvs...)
}

// Error writes an error level message using the logger from the context.
func Error(ctx context.Context, args ...any) {
	FromContext(ctx).Error(args...)
}

// Errorf writes a formatted error level message using the logger from the context.
func Errorf(ctx context.Context, format string, args ...any) {
	FromContext(ctx).Errorf(format, args...)
}

// ErrorKV writes a message and key-value pairs
// at the error level using the logger from the context.
func ErrorKV(ctx context.Context, message string, kvs ...any) {
	FromContext(ctx).Errorw(message, kvs...)
}

// Fatal writes a fatal error level message
// using the logger from the context and then calls os.Exit(1).
func Fatal(ctx context.Context, args ...any) {
	if handleFatal(ctx, func(l *zap.SugaredLogger) {
		l.Error(args...)
	}) {
		return
	}

	FromContext(ctx).Fatal(args...)
}

// Fatalf writes a formatted fatal error level message
// using the logger from the context and then calls os.Exit(1).
func Fatalf(ctx context.Context, format string, args ...any) {
	if handleFatal(ctx, func(l *zap.SugaredLogger) {
		l.Errorf(format, args...)
	}) {
		return
	}

	FromContext(ctx).Fatalf(format, args...)
}

// FatalKV writes a message and key-value pairs
// at the fatal error level using the logger from the context
// and then calls os.Exit(1).
func FatalKV(ctx context.Context, message string, kvs ...any) {
	if handleFatal(ctx, func(l *zap.SugaredLogger) {
		l.Errorw(message, kvs...)
	}) {
		return
	}

	FromContext(ctx).Fatalw(message, kvs...)
}

// Panic writes a panic level message
// using the logger from the context and then calls panic().
func Panic(ctx context.Context, args ...any) {
	FromContext(ctx).Panic(args...)
}

// Panicf writes a formatted panic level message
// using the logger from the context and then calls panic().
func Panicf(ctx context.Context, format string, args ...any) {
	FromContext(ctx).Panicf(format, args...)
}

// PanicKV writes a message and key-value pairs
// at the panic level using the logger from the context
// and then calls panic().
func PanicKV(ctx context.Context, message string, kvs ...any) {
	FromContext(ctx).Panicw(message, kvs...)
}

// handleFatal manages fatal error handling by checking if a custom fatal handler
// is registered and executing it instead of the default fatal behavior.
// This is primarily used for testing to prevent os.Exit calls during tests.
// Returns true if a custom handler was executed, false if normal fatal behavior should proceed.
func handleFatal(ctx context.Context, logFunc func(*zap.SugaredLogger)) bool {
	fatalHandlerMutex.Lock()

	atomicHandler := fatalHandler

	fatalHandlerMutex.Unlock()

	if atomicHandler == nil {
		return false
	}

	logFunc(FromContext(ctx))
	atomicHandler(1)

	return true
}
