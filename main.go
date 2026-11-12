package main

import (
	"fmt"
	"log/slog"
	"os"

	flag "github.com/spf13/pflag"

	"github.com/render-oss/render-mcp-server/cmd"
	"github.com/render-oss/render-mcp-server/pkg/cfg"
	"github.com/render-oss/render-mcp-server/pkg/logging"
)

func main() {
	// Define and parse command line flags
	versionFlag := flag.Bool("version", false, "Print version information and exit")
	flag.BoolVar(versionFlag, "v", false, "Print version information and exit")

	var transport string
	flag.StringVarP(&transport, "transport", "t", "stdio", "Transport type (stdio or http)")

	flag.Parse()

	if *versionFlag {
		fmt.Println("render-mcp-server version", cfg.Version)
		os.Exit(0)
	}

	level := slog.LevelInfo
	if value := os.Getenv("LOG_LEVEL"); value != "" {
		if err := level.UnmarshalText([]byte(value)); err != nil {
			fmt.Fprintf(os.Stderr, "Invalid LOG_LEVEL %q: %v\n", value, err)
			os.Exit(1)
		}
	} else if os.Getenv("LOGGING") == "1" {
		level = slog.LevelDebug
	}
	logging.SetDefault(logging.New(os.Stderr, level).WithFields(logging.OneOffField("app", "render-mcp-server")))
	logging.Info("starting server")

	// Start the server
	cmd.Serve(transport)
}
