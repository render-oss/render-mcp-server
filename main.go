package main

import (
	"flag"

	"github.com/render-oss/render-mcp-server/cmd"
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
	flag.Parse()
	config.InitRuntimeConfig(*includeSensitiveInfo)

	// Start the server
	cmd.Serve()
}
