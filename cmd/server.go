package cmd

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"os/signal"
	"slices"
	"syscall"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/render-oss/render-mcp-server/pkg/authn"
	"github.com/render-oss/render-mcp-server/pkg/blueprint"
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
	"github.com/render-oss/render-mcp-server/pkg/workflow"
	"github.com/render-oss/render-mcp-server/pkg/workspace"
)

const (
	httpSessionIdleTTL      = 30 * time.Minute
	maxHTTPRequestBodyBytes = 5 << 20
)

func Serve(transport string) *server.MCPServer {
	apiClient, err := client.NewDefaultClient()
	if err != nil {
		// TODO: We can't create a client unless we're logged in, so we should handle that error case.
		panic(err)
	}

	if transport == "http" {
		logging.Info("HTTP request body limit", logging.OneOffField("max_bytes", maxHTTPRequestBodyBytes))

		var sessionStore session.Store
		if redisURL, ok := os.LookupEnv("REDIS_URL"); ok {
			logging.Info("using Redis session store")
			sessionStore, err = session.NewRedisStore(redisURL)
			if err != nil {
				logging.WithError(err).Fatal("failed to initialize Redis session store")
			}
		} else {
			logging.Info("using in-memory session store")
			sessionStore = session.NewInMemoryStore()
		}
		mcpServer, httpTransport := newStreamableHTTPServer(apiClient, sessionStore)

		// OAuth resource-server support is opt-in via OAUTH_ENABLED;
		// pkg/oauth owns the gate. Fail at boot on misconfiguration.
		oauthCfg, err := oauth.FromEnv()
		if err != nil {
			logging.WithError(err).Fatal("OAuth configuration")
		}
		if oauthCfg.Enabled {
			// The resource URI must match the audience api mints per env; a
			// mismatch rejects every token, so log the resolved values.
			logging.Info("OAuth enabled",
				logging.OneOffField("resource", oauthCfg.CanonicalResourceURI),
				logging.OneOffField("authorization_server", oauthCfg.AuthorizationServerURL),
				logging.OneOffField("api_key_passthrough", oauthCfg.APIKeyPassthrough),
			)
		} else {
			logging.Info("OAuth disabled")
		}
		mux := newHTTPMux(httpTransport, oauthCfg, os.Getenv("OPENAI_VERIFICATION_TOKEN"))

		httpServer := &http.Server{
			Addr: ":10000",
			Handler: withMiddleware(
				mux,
				logging.RequestLoggerMiddleware,
				httpcontext.Middleware,
				logging.HTTPAccessLogMiddleware,
			),
			ReadTimeout: 5 * time.Second,
		}
		err = httpServer.ListenAndServe()
		if err != nil {
			logging.WithError(err).Fatal("Starting Streamable server")
		}
		return mcpServer
	}

	mcpServer, stdioTransport := newStdioServer(apiClient)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	// Listen propagates this logger to the contexts passed to our SDK hooks and tool handlers.
	ctx = logging.ContextWithDefaultLogger(ctx)
	err = stdioTransport.Listen(ctx, os.Stdin, os.Stdout)
	if err != nil && !errors.Is(err, context.Canceled) {
		logging.WithError(err).Fatal("Starting STDIO server")
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
	transportLogger := logging.InfoAsDebugLogger()

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
	tools = append(tools, blueprint.Tools(c)...)

	resolver := workspace.NewResolver(c)
	scopeTools := func(tools []server.ServerTool) []server.ServerTool {
		return workspace.ScopeTools(resolver, workspace.AddWorkspaceIDParam(tools...)...)
	}
	tools = scopeTools(tools)
	// Bound workspace resolution and all Workflows API calls together.
	workflowTools := workflow.Tools(c, workflowOptions())
	workflowTools = workflow.WithLimits(scopeTools(workflowTools))
	return append(tools, workflowTools...)
}

// newHTTPMux serves /mcp behind OAuth middleware and a request body limit,
// alongside the RFC 9728 metadata routes. With OAuth disabled, authentication
// passes through and metadata routes return 404.
// openAIToken, when set, serves the OpenAI app challenge.
func newHTTPMux(
	mcpHandler http.Handler,
	oauthCfg oauth.Config,
	openAIToken string,
) *http.ServeMux {
	oauthMiddleware := oauth.Middleware(oauthCfg, oauth.NewIntrospector(
		oauthCfg.AuthorizationServerURL,
		oauthCfg.APIAuthToken,
		oauth.DefaultIntrospectionCacheTTL,
	))

	mux := http.NewServeMux()
	mux.Handle("/mcp", withMiddleware(
		mcpHandler,
		oauthMiddleware,
		requestBodyLimitMiddleware(maxHTTPRequestBodyBytes),
	))
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

// withMiddleware applies middleware in request execution order.
// withMiddleware(handler, A, B, C): request -> A -> B -> C -> handler
func withMiddleware(handler http.Handler, middleware ...func(http.Handler) http.Handler) http.Handler {
	for _, wrap := range slices.Backward(middleware) {
		handler = wrap(handler)
	}
	return handler
}

func requestBodyLimitMiddleware(limitBytes int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body := &limitedRequestBody{ReadCloser: http.MaxBytesReader(w, r.Body, limitBytes)}
			r.Body = body
			next.ServeHTTP(w, r)
			if body.exceeded {
				logging.From(r.Context()).Warn("HTTP request body limit exceeded",
					logging.OneOffField("method", r.Method),
					logging.OneOffField("path", r.URL.Path),
					logging.OneOffField("limit_bytes", limitBytes),
					logging.OneOffField("content_length", r.ContentLength),
				)
			}
		})
	}
}

type limitedRequestBody struct {
	io.ReadCloser
	exceeded bool
}

func (b *limitedRequestBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
		b.exceeded = true
	}
	return n, err
}
