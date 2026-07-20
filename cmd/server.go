package cmd

import (
	"log"
	"net/http"
	"os"
	"time"

	"github.com/mark3labs/mcp-go/server"
	mcputil "github.com/mark3labs/mcp-go/util"
	"github.com/render-oss/render-mcp-server/pkg/authn"
	"github.com/render-oss/render-mcp-server/pkg/cfg"
	"github.com/render-oss/render-mcp-server/pkg/client"
	"github.com/render-oss/render-mcp-server/pkg/deploy"
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
)

func Serve(transport string) *server.MCPServer {
	mcpServerOpts := []server.ServerOption{}
	if hooks := logging.NewHooks(); hooks != nil {
		mcpServerOpts = append(mcpServerOpts, server.WithHooks(hooks))
	}

	// Create MCP server
	s := server.NewMCPServer(
		"render-mcp-server",
		cfg.Version,
		mcpServerOpts...,
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
		streamableServer := server.NewStreamableHTTPServer(s,
			server.WithLogger(mcputil.DefaultLogger()),
			server.WithHTTPContextFunc(multicontext.MultiHTTPContextFunc(
				session.ContextWithHTTPSession(sessionStore),
				authn.ContextWithAPITokenFromHeader,
				httpcontext.ContextWithHTTPRequest,
			)),
		)

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
		mux := newHTTPMux(streamableServer, oauthCfg, os.Getenv("OPENAI_VERIFICATION_TOKEN"))

		httpServer := &http.Server{
			Addr:        ":10000",
			Handler:     logging.HTTPMiddleware(mux),
			ReadTimeout: 5 * time.Second,
		}
		err = httpServer.ListenAndServe()
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
