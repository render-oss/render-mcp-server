package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/render-oss/render-mcp-server/cmd"
	"github.com/render-oss/render-mcp-server/pkg/cfg"
	"github.com/render-oss/render-mcp-server/pkg/config"
)

func main() {
	// Define and parse command line flags
	includeSensitiveInfo := flag.Bool(
		"include-sensitive-info",
		false,
		"Include sensitive information in responses. Turn this on if you don't mind including "+
			"potentially sensitive information such as environment variables or database credentials "+
			"in your LLM context.",
	)

	versionFlag := flag.Bool("version", false, "Print version information and exit")
	flag.BoolVar(versionFlag, "v", false, "Print version information and exit")

	flag.Parse()

	// Print version and exit if version flag is provided
	if *versionFlag {
		fmt.Println("render-mcp-server version", cfg.Version)
		os.Exit(0)
	}

	config.InitRuntimeConfig(*includeSensitiveInfo)

	// Start the server
	cmd.Serve()
}
