package logging

import (
	"bytes"
	"log"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestInfoAsDebugLogger(t *testing.T) {
	previous := defaultLogger
	previousSlog := slog.Default()
	previousWriter, previousFlags := log.Writer(), log.Flags()
	t.Cleanup(func() {
		defaultLogger = previous
		slog.SetDefault(previousSlog)
		log.SetOutput(previousWriter)
		log.SetFlags(previousFlags)
	})

	t.Run("info diagnostics are hidden at the application info level", func(t *testing.T) {
		var output bytes.Buffer
		SetDefault(New(&output, slog.LevelInfo))
		logger := InfoAsDebugLogger()

		logger.Info("diagnostic")
		logger.Warn("warning")
		logger.Error("failure")

		records := readLogRecords(t, &output)
		require.Len(t, records, 2)
		require.Equal(t, "WARN", records[0]["level"])
		require.Equal(t, "warning", records[0]["msg"])
		require.Equal(t, "ERROR", records[1]["level"])
		require.Equal(t, "failure", records[1]["msg"])
	})

	t.Run("info diagnostics are emitted as debug when enabled", func(t *testing.T) {
		var output bytes.Buffer
		SetDefault(New(&output, slog.LevelDebug))

		InfoAsDebugLogger().Info("diagnostic")

		records := readLogRecords(t, &output)
		require.Len(t, records, 1)
		require.Equal(t, "DEBUG", records[0]["level"])
		require.Equal(t, "diagnostic", records[0]["msg"])
	})
}
