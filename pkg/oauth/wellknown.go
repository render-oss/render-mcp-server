package oauth

import (
	"encoding/json"
	"net/http"
)

// ProtectedResourceMetadata is the RFC 9728 protected-resource metadata
// document.
type ProtectedResourceMetadata struct {
	Resource               string   `json:"resource"`
	AuthorizationServers   []string `json:"authorization_servers"`
	BearerMethodsSupported []string `json:"bearer_methods_supported"`
}

// HandleProtectedResourceMetadata serves the RFC 9728 discovery document that
// clients fetch after a 401 to locate the authorization server. Register it
// at every path in Config.MetadataPaths. Serves 404 when OAuth is disabled so
// nothing is advertised before the deployment opts in.
func HandleProtectedResourceMetadata(cfg Config) http.HandlerFunc {
	if !cfg.Enabled {
		return http.NotFound
	}
	// Marshaled once at startup; the document is static, so the returned
	// handler only writes these bytes.
	body, _ := json.Marshal(ProtectedResourceMetadata{
		Resource:             cfg.CanonicalResourceURI,
		AuthorizationServers: []string{cfg.AuthorizationServerURL},
		// Tokens are accepted from the Authorization header only (RFC 6750 §2.1).
		BearerMethodsSupported: []string{"header"},
	})
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "public, max-age=300")
		_, _ = w.Write(body)
	}
}
