package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/openpak/shop-core/internal/store"
)

// stub embeds the interface so a test only implements what it calls.
type stub struct {
	Store
	retired   string
	published *bool
}

func (s *stub) Ping(context.Context) error { return nil }
func (s *stub) SetPublished(_ context.Context, console, _ string, p bool, _ string) error {
	if console == "switch" {
		return store.ErrReadOnly
	}
	s.published = &p
	return nil
}
func (s *stub) Retire(_ context.Context, _, id, reason, _ string) error {
	s.retired = id + ":" + reason
	return nil
}
func (s *stub) Broken(context.Context, string) (map[string]store.BrokenEntry, error) {
	return map[string]store.BrokenEntry{"pkg": {Context: "c"}}, nil
}

func do(t *testing.T, srv *Server, method, path, key, body string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	srv.Register(mux)
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if key != "" {
		req.Header.Set("X-Internal-Key", key)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestAuth(t *testing.T) {
	srv := &Server{Store: &stub{}, Key: "k"}
	if c := do(t, srv, "GET", "/healthz", "", "").Code; c != 200 {
		t.Fatalf("healthz open: %d", c)
	}
	if c := do(t, srv, "GET", "/v1/shop/wiiu/broken", "", "").Code; c != 401 {
		t.Fatalf("no key: %d", c)
	}
	if c := do(t, srv, "GET", "/v1/shop/wiiu/broken", "bad", "").Code; c != 401 {
		t.Fatalf("bad key: %d", c)
	}
	if c := do(t, srv, "GET", "/v1/shop/wiiu/broken", "k", "").Code; c != 200 {
		t.Fatalf("good key: %d", c)
	}
	if c := do(t, &Server{Store: &stub{}}, "GET", "/v1/shop/wiiu/broken", "x", "").Code; c != 503 {
		t.Fatalf("no configured key must refuse: %d", c)
	}
}

func TestReadOnlyAndValidation(t *testing.T) {
	st := &stub{}
	srv := &Server{Store: st, Key: "k"}
	if r := do(t, srv, "POST", "/v1/shop/switch/titles/0100abcd12340000/publish", "k", ""); r.Code != 409 || !strings.Contains(r.Body.String(), "read_only") {
		t.Fatalf("switch publish: %d %s", r.Code, r.Body)
	}
	if r := do(t, srv, "POST", "/v1/shop/wiiu/titles/00050000FE000001/publish", "k", ""); r.Code != 200 || st.published == nil || !*st.published {
		t.Fatalf("wiiu publish: %d %s", r.Code, r.Body)
	}
	if r := do(t, srv, "POST", "/v1/shop/wiiu/titles/nothex/publish", "k", ""); r.Code != 400 {
		t.Fatalf("bad id: %d", r.Code)
	}
	if r := do(t, srv, "GET", "/v1/shop/WIIU/titles", "k", ""); r.Code != 400 {
		t.Fatalf("bad console: %d", r.Code)
	}
	if r := do(t, srv, "POST", "/v1/shop/wiiu/titles/00050000fe000001/retire", "k", `{"reason":"upstream gone"}`); r.Code != 200 || st.retired != "00050000fe000001:upstream gone" {
		t.Fatalf("retire: %d %s %q", r.Code, r.Body, st.retired)
	}
	if r := do(t, srv, "PUT", "/v1/shop/wiiu/titles/00050000fe000001", "k", `{"name":"x"}`); r.Code != 400 {
		t.Fatalf("put needs fields: %d", r.Code)
	}
	if r := do(t, srv, "POST", "/v1/shop/switch/import", "k", ""); r.Code != 503 {
		t.Fatalf("import without source: %d", r.Code)
	}
}
