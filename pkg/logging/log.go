package logging

import (
	"context"
	"io"
	"log/slog"
	"os"
)

const errorField = "error"

var defaultLogger = Logger{logger: slog.Default(), ctx: context.Background()}

type Field = slog.Attr

func OneOffField(key string, value any) Field {
	return slog.Any(key, value)
}

// Logger carries immutable fields and the context used when emitting a record.
type Logger struct {
	logger *slog.Logger
	ctx    context.Context
}

func New(output io.Writer, level slog.Level) Logger {
	return Logger{
		logger: slog.New(slog.NewJSONHandler(output, &slog.HandlerOptions{Level: level})),
		ctx:    context.Background(),
	}
}

// SetDefault configures application logging, including the standard log package.
// It must not run concurrently with logging or other logger access.
// Call it during startup, before constructing servers or starting goroutines.
func SetDefault(logger Logger) {
	logger.ctx = context.Background()
	defaultLogger = logger
	slog.SetDefault(logger.logger)
}

// WithFields returns a logger with additional fields without changing the receiver.
// As with slog.With, duplicate keys are retained rather than replaced.
func (l Logger) WithFields(fields ...Field) Logger {
	args := make([]any, len(fields))
	for i, field := range fields {
		args[i] = field
	}
	l.logger = l.logger.With(args...)
	return l
}

func (l Logger) WithError(err error) Logger {
	return l.WithFields(OneOffField(errorField, err))
}

func (l Logger) Debug(message string, fields ...Field) { l.log(slog.LevelDebug, message, fields...) }
func (l Logger) Info(message string, fields ...Field)  { l.log(slog.LevelInfo, message, fields...) }
func (l Logger) Warn(message string, fields ...Field)  { l.log(slog.LevelWarn, message, fields...) }
func (l Logger) Error(message string, fields ...Field) { l.log(slog.LevelError, message, fields...) }

func (l Logger) Fatal(message string, fields ...Field) {
	l.Error(message, fields...)
	os.Exit(1)
}

func (l Logger) log(level slog.Level, message string, fields ...Field) {
	l.logger.LogAttrs(l.ctx, level, message, fields...)
}

func WithError(err error) Logger { return defaultLogger.WithError(err) }

func Debug(message string, fields ...Field) { defaultLogger.Debug(message, fields...) }
func Info(message string, fields ...Field)  { defaultLogger.Info(message, fields...) }
func Warn(message string, fields ...Field)  { defaultLogger.Warn(message, fields...) }
func Error(message string, fields ...Field) { defaultLogger.Error(message, fields...) }
func Fatal(message string, fields ...Field) { defaultLogger.Fatal(message, fields...) }

// InfoAsDebugLogger uses the application's configured handler,
// mapping INFO records to DEBUG.
func InfoAsDebugLogger() *slog.Logger {
	return slog.New(infoAsDebugHandler{Handler: slog.Default().Handler()})
}

type infoAsDebugHandler struct {
	slog.Handler
}

func infoAsDebugLevel(level slog.Level) slog.Level {
	if level == slog.LevelInfo {
		return slog.LevelDebug
	}
	return level
}

func (h infoAsDebugHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.Handler.Enabled(ctx, infoAsDebugLevel(level))
}

func (h infoAsDebugHandler) Handle(ctx context.Context, record slog.Record) error {
	record.Level = infoAsDebugLevel(record.Level)
	return h.Handler.Handle(ctx, record)
}

func (h infoAsDebugHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return infoAsDebugHandler{Handler: h.Handler.WithAttrs(attrs)}
}

func (h infoAsDebugHandler) WithGroup(name string) slog.Handler {
	return infoAsDebugHandler{Handler: h.Handler.WithGroup(name)}
}
