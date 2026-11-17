package ring

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/alexandrmotologa/kvring/internal/config"
)

func TestAffinityExtractor(t *testing.T) {
	cfg := config.AffinityConfig{
		HeaderKey:       "X-Session-ID",
		CookieKey:       "session_id",
		EnablePrefix:    true,
		PrefixLength:    64,
		FallbackHeaders: []string{"X-User-ID", "Authorization"},
	}
	extractor := NewAffinityExtractor(cfg)

	// Case 1: Custom Header
	req1 := httptest.NewRequest("POST", "/v1/chat/completions", nil)
	req1.Header.Set("X-Session-ID", "session-header-abc")
	if key := extractor.Extract(req1); key != "session-header-abc" {
		t.Errorf("expected session-header-abc, got %s", key)
	}

	// Case 2: Query param
	req2 := httptest.NewRequest("POST", "/v1/chat/completions?session_id=query-xyz", nil)
	if key := extractor.Extract(req2); key != "query-xyz" {
		t.Errorf("expected query-xyz, got %s", key)
	}

	// Case 3: Cookie
	req3 := httptest.NewRequest("POST", "/v1/chat/completions", nil)
	req3.AddCookie(&http.Cookie{Name: "session_id", Value: "cookie-val-99"})
	if key := extractor.Extract(req3); key != "cookie-val-99" {
		t.Errorf("expected cookie-val-99, got %s", key)
	}

	// Case 4: JSON body prompt prefix
	bodyJSON := `{"messages":[{"role":"system","content":"You are a helpful assistant specialized in coding."},{"role":"user","content":"Explain Go interfaces"}]}`
	req4 := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(bodyJSON))
	req4.Header.Set("Content-Type", "application/json")
	key4 := extractor.Extract(req4)
	if !strings.HasPrefix(key4, "prefix:") {
		t.Errorf("expected prefix: hash key, got %s", key4)
	}

	// Verify request body was preserved for downstream reading
	buf := new(bytes.Buffer)
	_, _ = buf.ReadFrom(req4.Body)
	if buf.String() != bodyJSON {
		t.Errorf("expected body to be preserved, got: %s", buf.String())
	}
}
