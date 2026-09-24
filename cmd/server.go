package cmd

import (
	"context"
	"errors"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/render-oss/render-mcp-server/pkg/authn"
	"github.com/render-oss/render-mcp-server/pkg/cfg"
	"github.com/render-oss/render-mcp-server/pkg/client"
	"github.com/render-oss/render-mcp-server/pkg/deploy"
	"github.com/render-oss/render-mcp-server/pkg/event"
	"github.com/render-oss/render-mcp-server/pkg/httpcontext"
	"github.com/render-oss/render-mcp-server/pkg/keyvalue"
	"github.com/render-oss/render-mcp-server/pkg/logging"
	"github.com/render-oss/render-mcp-server/pkg/logs"
	"github.com/render-oss/render-mcp-server/pkg/metrics"
	"github.com/render-oss/render-mcp-server/pkg/multicontext"
	"github.com/render-oss/render-mcp-server/pkg/oauth"
	"github.com/render-oss/render-mcp-server/pkg/owner"
	"github.com/render-oss/render-mcp-server/pkg/postgres"
	"github.com/render-oss/render-mcp-server/pkg/service"
	"github.com/render-oss/render-mcp-server/pkg/session"
	"github.com/render-oss/render-mcp-server/pkg/workspace"
)

const httpSessionIdleTTL = 30 * time.Minute

func Serve(transport string) *server.MCPServer {
	apiClient, err := client.NewDefaultClient()
	if err != nil {
		// TODO: We can't create a client unless we're logged in, so we should handle that error case.
		panic(err)
	}

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
		mcpServer, httpTransport := newStreamableHTTPServer(apiClient, sessionStore)

		// OAuth resource-server support is opt-in via OAUTH_ENABLED;
		// pkg/oauth owns the gate. Fail at boot on misconfiguration.
		oauthCfg, err := oauth.FromEnv()
		if err != nil {
			log.Fatalf("OAuth configuration: %v", err)
		}
		if oauthCfg.Enabled {
			// The resource URI must match the audience api mints per env; a
			// mismatch rejects every token, so log the resolved values.
			log.Printf("OAuth enabled: resource=%s authorization-server=%s api-key-passthrough=%t",
				oauthCfg.CanonicalResourceURI, oauthCfg.AuthorizationServerURL, oauthCfg.APIKeyPassthrough)
		} else {
			log.Print("OAuth disabled")
		}
		mux := newHTTPMux(httpTransport, oauthCfg, os.Getenv("OPENAI_VERIFICATION_TOKEN"))

		httpServer := &http.Server{
			Addr:        ":10000",
			Handler:     logging.HTTPMiddleware(mux),
			ReadTimeout: 5 * time.Second,
		}
		err = httpServer.ListenAndServe()
		if err != nil {
			log.Fatalf("Starting Streamable server: %v\n:", err)
		}
		return mcpServer
	}

	mcpServer, stdioTransport := newStdioServer(apiClient)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	err = stdioTransport.Listen(ctx, os.Stdin, os.Stdout)
	if err != nil && !errors.Is(err, context.Canceled) {
		log.Fatalf("Starting STDIO server: %v\n", err)
	}
	return mcpServer
}

func newMCPServer(apiClient *client.ClientWithResponses) *server.MCPServer {
	var mcpServerOpts []server.ServerOption
	hooks := new(server.Hooks)
	logging.AddHooks(hooks)
	mcpServerOpts = append(mcpServerOpts, server.WithHooks(hooks))

	mcpServer := server.NewMCPServer(
		"render-mcp-server",
		cfg.Version,
		mcpServerOpts...,
	)
	mcpServer.AddTools(owner.Tools(apiClient)...)
	mcpServer.AddTools(buildWorkspaceScopedTools(apiClient)...)
	return mcpServer
}

func newStreamableHTTPServer(apiClient *client.ClientWithResponses, sessionStore session.Store) (*server.MCPServer, *server.StreamableHTTPServer) {
	mcpServer := newMCPServer(apiClient)
	transportLogger := slog.Default()

	httpTransport := server.NewStreamableHTTPServer(mcpServer,
		server.WithStreamableHTTPLogger(transportLogger),
		// Our HTTP workspace store is keyed by MCP session ID. Under protocol
		// 2026-07-28 or later, the SDK supplies an empty session ID, so clients
		// using select_workspace would overwrite one another's stored selection.
		// Restrict HTTP to session-based protocols until workspace selection
		// can safely handle sessionless requests.
		server.WithStreamableHTTPProtocolVersions(mcp.LegacyProtocolVersions()...),
		// mcp-go SDK v1.0.0 disables idle session expiry by default. Session-based
		// protocols ask clients to send DELETE when finished, but allow server-side
		// expiry. Use a provisional 30-minute timeout to reclaim abandoned session
		// state. Revisit if clients need that state preserved across longer idle periods.
		server.WithSessionIdleTTL(httpSessionIdleTTL),
		server.WithHTTPContextFunc(multicontext.MultiHTTPContextFunc(
			session.ContextWithHTTPSession(sessionStore),
			authn.ContextWithAPITokenFromHeader,
			httpcontext.ContextWithHTTPRequest,
		)),
	)
	return mcpServer, httpTransport
}

func newStdioServer(apiClient *client.ClientWithResponses) (*server.MCPServer, *server.StdioServer) {
	mcpServer := newMCPServer(apiClient)
	stdioTransport := server.NewStdioServer(mcpServer)

	opts := []server.StdioOption{
		// One worker preserves ordering while tool calls fit in the queue.
		// The SDK executes overflow calls concurrently; clients must wait for
		// dependent calls to finish before changing the selected workspace.
		server.WithWorkerPoolSize(1),
		server.WithStdioContextFunc(multicontext.MultiStdioContextFunc(
			session.ContextWithStdioSession,
			authn.ContextWithAPITokenFromConfig,
		)),
	}
	for _, opt := range opts {
		opt(stdioTransport)
	}

	return mcpServer, stdioTransport
}

func buildWorkspaceScopedTools(c *client.ClientWithResponses) []server.ServerTool {
	var tools []server.ServerTool
	tools = append(tools, service.Tools(c)...)
	tools = append(tools, deploy.Tools(c)...)
	tools = append(tools, event.Tools(c)...)
	tools = append(tools, postgres.Tools(c)...)
	tools = append(tools, keyvalue.Tools(c)...)
	tools = append(tools, logs.Tools(c)...)
	tools = append(tools, metrics.Tools(c)...)

	tools = workspace.AddWorkspaceIDParam(tools...)
	return workspace.ScopeTools(workspace.NewResolver(c), tools...)
}

// newHTTPMux serves /mcp behind the OAuth middleware plus the RFC 9728 metadata
// routes. When OAuth is disabled the middleware is identity and metadata 404s,
// so /mcp is unchanged. openAIToken, when set, serves the OpenAI app challenge.
func newHTTPMux(mcpHandler http.Handler, oauthCfg oauth.Config, openAIToken string) *http.ServeMux {
	oauthMiddleware := oauth.Middleware(oauthCfg, oauth.NewIntrospector(
		oauthCfg.AuthorizationServerURL,
		oauthCfg.APIAuthToken,
		oauth.DefaultIntrospectionCacheTTL,
	))

	mux := http.NewServeMux()
	mux.Handle("/mcp", oauthMiddleware(mcpHandler))
	metadata := oauth.HandleProtectedResourceMetadata(oauthCfg)
	for _, path := range oauthCfg.MetadataPaths() {
		mux.HandleFunc(path, metadata)
	}
	if openAIToken != "" {
		mux.HandleFunc("/.well-known/openai-apps-challenge", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte(openAIToken))
		})
	}
	return mux
}
