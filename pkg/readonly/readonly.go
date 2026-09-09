package readonly

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

const (
	// HeaderName enables read-only mode for an HTTP MCP session.
	HeaderName = "X-MCP-Readonly"

	// PermissionDeniedMessage is returned before a mutating or unclassified
	// tool handler can run in read-only mode.
	PermissionDeniedMessage = "permission denied: tool is unavailable in read-only mode"
)

type mode bool

const (
	fullAccess mode = false
	readOnly   mode = true
)

type modeContextKey struct{}

// FromContext reports whether the current request is in read-only mode.
// Contexts that do not come from the HTTP read-only middleware, including
// stdio requests, retain full access.
func FromContext(ctx context.Context) bool {
	value, _ := ctx.Value(modeContextKey{}).(mode)
	return value == readOnly
}

// ContextWithReadOnly records the effective access mode for a request.
func ContextWithReadOnly(ctx context.Context, enabled bool) context.Context {
	return context.WithValue(ctx, modeContextKey{}, mode(enabled))
}

// HTTPGuard validates the read-only header and binds its effective value to
// the MCP session returned by an initialize request.
type HTTPGuard struct {
	mu       sync.RWMutex
	sessions map[string]mode
}

func NewHTTPGuard() *HTTPGuard {
	return &HTTPGuard{sessions: make(map[string]mode)}
}

// Middleware enables strict, HTTP-only read-only mode. A session initialized
// as read-only must continue sending X-MCP-Readonly: true; changing or omitting
// it is rejected before the MCP handler runs.
func (g *HTTPGuard) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedMode, err := parseHeader(r.Header.Values(HeaderName))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		sessionID := r.Header.Get("Mcp-Session-Id")
		if sessionID != "" {
			g.mu.RLock()
			boundMode, ok := g.sessions[sessionID]
			g.mu.RUnlock()
			if ok && boundMode != requestedMode {
				http.Error(w, "X-MCP-Readonly must match the value used to initialize this MCP session", http.StatusConflict)
				return
			}
		}

		ctx := ContextWithReadOnly(r.Context(), requestedMode == readOnly)
		next.ServeHTTP(w, r.WithContext(ctx))

		if initializedSessionID := w.Header().Get("Mcp-Session-Id"); initializedSessionID != "" {
			g.mu.Lock()
			g.sessions[initializedSessionID] = requestedMode
			g.mu.Unlock()
		}
		if r.Method == http.MethodDelete && sessionID != "" {
			g.mu.Lock()
			delete(g.sessions, sessionID)
			g.mu.Unlock()
		}
	})
}

func parseHeader(values []string) (mode, error) {
	switch {
	case len(values) == 0:
		return fullAccess, nil
	case len(values) > 1:
		return fullAccess, errors.New("X-MCP-Readonly must appear once with the value true or false")
	}

	switch values[0] {
	case "true":
		return readOnly, nil
	case "false":
		return fullAccess, nil
	default:
		return fullAccess, errors.New("X-MCP-Readonly must be exactly true or false")
	}
}

// Policy uses the audited readOnlyHint annotations as the single source of
// truth for both discovery filtering and call-time enforcement.
type Policy struct {
	toolReadOnly map[string]bool
}

func NewPolicy(tools []server.ServerTool) (*Policy, error) {
	policy := &Policy{toolReadOnly: make(map[string]bool, len(tools))}
	for _, tool := range tools {
		name := tool.Tool.Name
		if name == "" {
			return nil, errors.New("tool classification has an empty name")
		}
		if _, exists := policy.toolReadOnly[name]; exists {
			return nil, fmt.Errorf("tool %q is registered more than once", name)
		}
		if tool.Tool.Annotations.ReadOnlyHint == nil {
			return nil, fmt.Errorf("tool %q has no explicit read-only classification", name)
		}
		policy.toolReadOnly[name] = *tool.Tool.Annotations.ReadOnlyHint
	}
	return policy, nil
}

// Filter hides mutating and unclassified tools during read-only discovery.
func (p *Policy) Filter(ctx context.Context, tools []mcp.Tool) []mcp.Tool {
	if !FromContext(ctx) {
		return tools
	}

	filtered := make([]mcp.Tool, 0, len(tools))
	for _, tool := range tools {
		if allowed, classified := p.toolReadOnly[tool.Name]; classified && allowed {
			filtered = append(filtered, tool)
		}
	}
	return filtered
}

// Middleware blocks direct invocation of mutating and unclassified tools
// before any tool-specific or workspace-scoping handler can run.
func (p *Policy) Middleware(next server.ToolHandlerFunc) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if FromContext(ctx) {
			allowed, classified := p.toolReadOnly[request.Params.Name]
			if !classified || !allowed {
				return mcp.NewToolResultError(PermissionDeniedMessage), nil
			}
		}
		return next(ctx, request)
	}
}
