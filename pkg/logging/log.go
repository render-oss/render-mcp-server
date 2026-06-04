package logging

import (
	"log"
	"os"
)

const envKey = "LOGGING"

var logger = log.New(os.Stderr, "render-mcp-server: ", log.LstdFlags)

func enabled() bool {
	return os.Getenv(envKey) == "1"
}

func Info(format string, args ...any) {
	if !enabled() {
		return
	}
	logger.Printf("INFO "+format, args...)
}

func Error(format string, args ...any) {
	if !enabled() {
		return
	}
	logger.Printf("ERROR "+format, args...)
}
