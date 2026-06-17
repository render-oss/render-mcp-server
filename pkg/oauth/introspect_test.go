package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type fakeAS struct {
	mu      sync.Mutex
	handler http.HandlerFunc
	calls   atomic.Int64
}

func (f *fakeAS) set(h http.HandlerFunc) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.handler = h
}

func (f *fakeAS) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.calls.Add(1)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.handler(w, r)
}

// testIntrospector returns an Introspector pointed at a fake authorization
// server that initially answers every introspection with active=false.
func testIntrospector(t *testing.T, serviceToken string) (*Introspector, *fakeAS) {
	t.Helper()
	as := &fakeAS{handler: respondJSON(`{"active": false}`)}
	srv := httptest.NewServer(as)
	t.Cleanup(srv.Close)
	return NewIntrospector(srv.URL, serviceToken, time.Minute), as
}

func respondJSON(body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}
}

func activeJSON(now time.Time, aud string) string {
	return fmt.Sprintf(
		`{"active":true,"sub":"user:1","client_id":"client-1","aud":%q,"exp":%d}`,
		aud, now.Add(30*time.Minute).Unix(),
	)
}

// setClock replaces the introspector's clock with a mutable fake; the
// returned function advances it.
func setClock(i *Introspector, start time.Time) func(time.Duration) {
	var mu sync.Mutex
	current := start
	i.now = func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		return current
	}
	return func(d time.Duration) {
		mu.Lock()
		defer mu.Unlock()
		current = current.Add(d)
	}
}

func TestIntrospector_RequestShape(t *testing.T) {
	i, as := testIntrospector(t, "svc-token")
	var got struct {
		method, path, contentType, authorization, userAgent, token string
	}
	as.set(func(w http.ResponseWriter, r *http.Request) {
		got.method = r.Method
		got.path = r.URL.Path
		got.contentType = r.Header.Get("Content-Type")
		got.authorization = r.Header.Get("Authorization")
		got.userAgent = r.Header.Get("User-Agent")
		require.NoError(t, r.ParseForm())
		got.token = r.PostForm.Get("token")
		_, _ = w.Write([]byte(`{"active": false}`))
	})

	_, err := i.Introspect(context.Background(), "some-token")

	require.NoError(t, err)
	require.Equal(t, http.MethodPost, got.method)
	require.Equal(t, "/v1/oauth/introspect", got.path)
	require.Equal(t, "application/x-www-form-urlencoded", got.contentType)
	require.Equal(t, "Bearer svc-token", got.authorization)
	require.Contains(t, got.userAgent, "render-mcp-server")
	require.Equal(t, "some-token", got.token)
}

func TestIntrospector_NoServiceTokenSendsNoAuthorization(t *testing.T) {
	i, as := testIntrospector(t, "")
	var mu sync.Mutex
	var authorization string
	as.set(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		authorization = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"active": false}`))
	})

	_, err := i.Introspect(context.Background(), "some-token")

	require.NoError(t, err)
	mu.Lock()
	defer mu.Unlock()
	require.Empty(t, authorization)
}

func TestIntrospector_DecodesActiveResponse(t *testing.T) {
	i, as := testIntrospector(t, "")
	now := time.Now()
	as.set(respondJSON(activeJSON(now, "https://mcp.example.com/mcp")))

	resp, err := i.Introspect(context.Background(), "token-1")

	require.NoError(t, err)
	require.True(t, resp.Active)
	require.Equal(t, "user:1", resp.Subject)
	require.Equal(t, "client-1", resp.ClientID)
	require.Equal(t, Audience{"https://mcp.example.com/mcp"}, resp.Audience)
	require.Equal(t, now.Add(30*time.Minute).Unix(), resp.Exp)
}

func TestIntrospector_CanonicalizesAudienceAtDecode(t *testing.T) {
	i, as := testIntrospector(t, "")
	as.set(respondJSON(`{"active":true,"aud":["HTTPS://MCP.Example.com:443/mcp","https://other.example.com/"]}`))

	resp, err := i.Introspect(context.Background(), "token-1")

	require.NoError(t, err)
	require.Equal(t,
		Audience{"https://mcp.example.com/mcp", "https://other.example.com"},
		resp.Audience)
}

func TestIntrospector_CachesActiveResults(t *testing.T) {
	i, as := testIntrospector(t, "")
	as.set(respondJSON(activeJSON(time.Now(), "https://mcp.example.com/mcp")))

	first, err := i.Introspect(context.Background(), "token-1")
	require.NoError(t, err)
	second, err := i.Introspect(context.Background(), "token-1")
	require.NoError(t, err)

	require.Equal(t, int64(1), as.calls.Load())
	require.Equal(t, first, second)
}

func TestIntrospector_CachesInactiveResults(t *testing.T) {
	i, as := testIntrospector(t, "")

	_, err := i.Introspect(context.Background(), "some-api-key")
	require.NoError(t, err)
	_, err = i.Introspect(context.Background(), "some-api-key")
	require.NoError(t, err)

	require.Equal(t, int64(1), as.calls.Load())
}

func TestIntrospector_DistinctTokensAreNotShared(t *testing.T) {
	i, as := testIntrospector(t, "")

	_, err := i.Introspect(context.Background(), "token-1")
	require.NoError(t, err)
	_, err = i.Introspect(context.Background(), "token-2")
	require.NoError(t, err)

	require.Equal(t, int64(2), as.calls.Load())
}

func TestIntrospector_CacheExpiresAfterTTL(t *testing.T) {
	i, as := testIntrospector(t, "")
	as.set(respondJSON(activeJSON(time.Now(), "https://mcp.example.com/mcp")))
	advance := setClock(i, time.Now())

	_, err := i.Introspect(context.Background(), "token-1")
	require.NoError(t, err)

	advance(61 * time.Second)

	_, err = i.Introspect(context.Background(), "token-1")
	require.NoError(t, err)
	require.Equal(t, int64(2), as.calls.Load())
}

func TestIntrospector_CacheCappedByTokenExpiry(t *testing.T) {
	i, as := testIntrospector(t, "")
	start := time.Now()
	body := fmt.Sprintf(`{"active":true,"aud":"https://mcp.example.com/mcp","exp":%d}`,
		start.Add(10*time.Second).Unix())
	as.set(respondJSON(body))
	advance := setClock(i, start)

	_, err := i.Introspect(context.Background(), "token-1")
	require.NoError(t, err)

	// Within the token's lifetime the cache is hit even though TTL is 60s...
	advance(5 * time.Second)
	_, err = i.Introspect(context.Background(), "token-1")
	require.NoError(t, err)
	require.Equal(t, int64(1), as.calls.Load())

	// ...but past exp the entry is gone, well before the TTL would lapse.
	advance(6 * time.Second)
	_, err = i.Introspect(context.Background(), "token-1")
	require.NoError(t, err)
	require.Equal(t, int64(2), as.calls.Load())
}

func TestIntrospector_ActiveButAlreadyExpiredIsNotCached(t *testing.T) {
	i, as := testIntrospector(t, "")
	body := fmt.Sprintf(`{"active":true,"aud":"https://mcp.example.com/mcp","exp":%d}`,
		time.Now().Add(-time.Minute).Unix())
	as.set(respondJSON(body))

	_, err := i.Introspect(context.Background(), "token-1")
	require.NoError(t, err)
	_, err = i.Introspect(context.Background(), "token-1")
	require.NoError(t, err)

	require.Equal(t, int64(2), as.calls.Load())
}

func TestIntrospector_ErrorsAreNotCached(t *testing.T) {
	i, as := testIntrospector(t, "")
	as.set(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	_, err := i.Introspect(context.Background(), "token-1")
	require.ErrorContains(t, err, "status 500")

	as.set(respondJSON(`{"active": false}`))
	resp, err := i.Introspect(context.Background(), "token-1")
	require.NoError(t, err)
	require.False(t, resp.Active)
	require.Equal(t, int64(2), as.calls.Load())
}

func TestIntrospector_UnauthorizedIsAnError(t *testing.T) {
	i, as := testIntrospector(t, "bad-token")
	as.set(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})

	_, err := i.Introspect(context.Background(), "token-1")

	require.ErrorContains(t, err, "rejected the service token")
}

func TestIntrospector_MalformedResponseIsAnError(t *testing.T) {
	i, as := testIntrospector(t, "")
	as.set(respondJSON(`{"active": tru`))

	_, err := i.Introspect(context.Background(), "token-1")

	require.ErrorContains(t, err, "decode introspection response")
}

func TestIntrospector_TransportErrorIsAnError(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	srv.Close()
	i := NewIntrospector(srv.URL, "", time.Minute)

	_, err := i.Introspect(context.Background(), "token-1")

	require.ErrorContains(t, err, "introspection request")
}

func TestIntrospector_ConcurrentRequestsCollapseToOneCall(t *testing.T) {
	i, as := testIntrospector(t, "")
	release := make(chan struct{})
	as.set(func(w http.ResponseWriter, _ *http.Request) {
		<-release
		_, _ = w.Write([]byte(activeJSON(time.Now(), "https://mcp.example.com/mcp")))
	})

	const n = 8
	var wg sync.WaitGroup
	results := make([]IntrospectionResponse, n)
	errs := make([]error, n)
	for k := range n {
		wg.Go(func() {
			results[k], errs[k] = i.Introspect(context.Background(), "token-1")
		})
	}
	// Give every goroutine time to join the in-flight call, then release it.
	time.Sleep(50 * time.Millisecond)
	close(release)
	wg.Wait()

	require.Equal(t, int64(1), as.calls.Load())
	for k := range n {
		require.NoError(t, errs[k])
		require.True(t, results[k].Active)
	}
}

func TestAudienceUnmarshal(t *testing.T) {
	t.Run("string", func(t *testing.T) {
		var a Audience
		require.NoError(t, json.Unmarshal([]byte(`"https://mcp.example.com/mcp"`), &a))
		require.Equal(t, Audience{"https://mcp.example.com/mcp"}, a)
	})

	t.Run("array", func(t *testing.T) {
		var a Audience
		require.NoError(t, json.Unmarshal([]byte(`["a","b"]`), &a))
		require.Equal(t, Audience{"a", "b"}, a)
	})

	t.Run("null", func(t *testing.T) {
		var a Audience
		require.NoError(t, json.Unmarshal([]byte(`null`), &a))
		require.Empty(t, a)
	})

	t.Run("number is rejected", func(t *testing.T) {
		var a Audience
		require.ErrorContains(t, json.Unmarshal([]byte(`42`), &a), "aud must be a string or array of strings")
	})
}
