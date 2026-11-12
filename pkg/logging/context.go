package logging

import (
	"context"
	"net/http"
	"runtime"
	"sync"
)

var missingLoggerWarning sync.Once

func warnMissingLogger() {
	// Skip this helper and From to identify the caller missing a logger.
	_, file, line, _ := runtime.Caller(2)
	missingLoggerWarning.Do(func() {
		defaultLogger.Warn("logging.From called without a context logger; using default application logger",
			OneOffField("file", file),
			OneOffField("line", line),
		)
	})
}

type loggerKey struct{}

// From returns the logger installed in ctx. A missing logger warns once per
// process and falls back to the application logger.
// The returned logger emits with ctx, including values added since it was installed.
func From(ctx context.Context, fields ...Field) Logger {
	logger, ok := ctx.Value(loggerKey{}).(Logger)
	if !ok {
		warnMissingLogger()
		logger = defaultLogger
	}
	logger.ctx = ctx
	return logger.WithFields(fields...)
}

func ContextWithLogger(ctx context.Context, logger Logger) context.Context {
	logger.ctx = context.Background()
	return context.WithValue(ctx, loggerKey{}, logger)
}

// ContextWithDefaultLogger installs the application's startup-configured logger.
func ContextWithDefaultLogger(ctx context.Context) context.Context {
	return ContextWithLogger(ctx, defaultLogger)
}

func ContextWithFields(ctx context.Context, fields ...Field) context.Context {
	return ContextWithLogger(ctx, From(ctx, fields...))
}

// RequestLoggerMiddleware installs the application's default logger in the request context.
func RequestLoggerMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := ContextWithDefaultLogger(r.Context())
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
