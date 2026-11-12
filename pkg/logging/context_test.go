package logging

import (
	"bytes"
	"errors"
	"log/slog"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestContextWithFields(t *testing.T) {
	var output bytes.Buffer
	logger := New(&output, slog.LevelInfo).WithFields(OneOffField("workspace", "tea-test"))
	parent := ContextWithLogger(t.Context(), logger)
	child := ContextWithFields(parent, OneOffField("tool", "deploy"))

	From(child).WithError(errors.New("deploy failed")).Error("child")
	From(parent).Info("parent")

	records := readLogRecords(t, &output)
	require.Len(t, records, 2)
	require.Equal(t, "tea-test", records[0]["workspace"])
	require.Equal(t, "deploy", records[0]["tool"])
	require.Equal(t, "deploy failed", records[0]["error"])
	require.Equal(t, "tea-test", records[1]["workspace"])
	require.NotContains(t, records[1], "tool")
	require.NotContains(t, records[1], "error")
}

func TestFromMissingLogger(t *testing.T) {
	var output bytes.Buffer
	previous := defaultLogger
	defaultLogger = New(&output, slog.LevelInfo)
	missingLoggerWarning = sync.Once{}
	t.Cleanup(func() {
		defaultLogger = previous
		missingLoggerWarning = sync.Once{}
	})
	ctx := t.Context()

	var lookups sync.WaitGroup
	for range 8 {
		lookups.Go(func() { From(ctx) })
	}
	lookups.Wait()
	From(ctx).Info("fallback still logs")

	records := readLogRecords(t, &output)
	require.Len(t, records, 2)
	require.Equal(t, "WARN", records[0]["level"])
	require.Contains(t, records[0]["msg"], "without a context logger")
	require.Contains(t, records[0]["file"], "context_test.go")
	require.Greater(t, records[0]["line"], float64(0))
	require.Equal(t, "fallback still logs", records[1]["msg"])
}
