package cmd

import (
	"log"
	"net/http"
	"os"
	"time"

	"github.com/mark3labs/mcp-go/server"
	"github.com/render-oss/render-mcp-server/pkg/authn"
	"github.com/render-oss/render-mcp-server/pkg/cfg"
	"github.com/render-oss/render-mcp-server/pkg/client"
	"github.com/render-oss/render-mcp-server/pkg/deploy"
	"github.com/render-oss/render-mcp-server/pkg/httpcontext"
	"github.com/render-oss/render-mcp-server/pkg/keyvalue"
	"github.com/render-oss/render-mcp-server/pkg/logs"
	"github.com/render-oss/render-mcp-server/pkg/metrics"
	"github.com/render-oss/render-mcp-server/pkg/multicontext"
	"github.com/render-oss/render-mcp-server/pkg/owner"
	"github.com/render-oss/render-mcp-server/pkg/postgres"
	"github.com/render-oss/render-mcp-server/pkg/service"
	"github.com/render-oss/render-mcp-server/pkg/session"
)

const serverInstructions = `This server manages resources on Render (https://render.com).

Workspace selection is required before most actions. Tools that operate on services, ` +
	"deploys, postgres databases, key-value stores, logs, or metrics need a workspace to be " +
	"selected for the current session.\n\n" +
	`Workspace flow:
  1. If unsure whether a workspace is selected, call ` + "`get_selected_workspace`" + `.
  2. If none is selected (or a tool returns a "no workspace selected" error), call ` +
	"`list_workspaces`" + ` to see available workspaces.
  3. Ask the user which workspace to use. NEVER pick one yourself — selecting the wrong ` +
	`workspace can cause destructive actions on unintended resources.
  4. Once the user confirms, call ` + "`select_workspace`" + ` with the matching ownerID, then ` +
	`retry the original tool call.`

func Serve(transport string) *server.MCPServer {
	// Create MCP server
	s := server.NewMCPServer(
		"render-mcp-server",
		cfg.Version,
		server.WithInstructions(serverInstructions),
	)

	c, err := client.NewDefaultClient()
	if err != nil {
		// TODO: We can't create a client unless we're logged in, so we should handle that error case.
		panic(err)
	}

	s.AddTools(owner.Tools(c)...)
	s.AddTools(service.Tools(c)...)
	s.AddTools(deploy.Tools(c)...)
	s.AddTools(postgres.Tools(c)...)
	s.AddTools(keyvalue.Tools(c)...)
	s.AddTools(logs.Tools(c)...)
	s.AddTools(metrics.Tools(c)...)

	if transport == "http" {
		var sessionStore session.Store
		if redisURL, ok := os.LookupEnv("REDIS_URL"); ok {
			log.Print("using Redis session store\n")
			sessionStore, err = session.NewRedisStore(redisURL)
			if err != nil {
				log.Fatalf("failed to initialize Redis session store: %v", err)
			}
		} else {
			log.Print("using in-memory session store\n")
			sessionStore = session.NewInMemoryStore()
		}
		streamableServer := server.NewStreamableHTTPServer(s, server.WithHTTPContextFunc(multicontext.MultiHTTPContextFunc(
			session.ContextWithHTTPSession(sessionStore),
			authn.ContextWithAPITokenFromHeader,
			httpcontext.ContextWithHTTPRequest,
		)))

		mux := http.NewServeMux()
		mux.Handle("/mcp", streamableServer)
		if token := os.Getenv("OPENAI_VERIFICATION_TOKEN"); token != "" {
			mux.HandleFunc("/.well-known/openai-apps-challenge", func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/plain")
				w.Write([]byte(token))
			})
		}

		httpServer := &http.Server{
			Addr:        ":10000",
			Handler:     mux,
			ReadTimeout: 5 * time.Second,
		}
		err := httpServer.ListenAndServe()
		if err != nil {
			log.Fatalf("Starting Streamable server: %v\n:", err)
		}
	} else {
		err := server.ServeStdio(s, server.WithStdioContextFunc(multicontext.MultiStdioContextFunc(
			session.ContextWithStdioSession,
			authn.ContextWithAPITokenFromConfig,
		)))
		if err != nil {
			log.Fatalf("Starting STDIO server: %v\n", err)
		}
	}

	return s
}
