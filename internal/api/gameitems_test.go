package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGameItemsRelay(t *testing.T) {
	var gotPath, gotKey, gotQuery, gotBody string
	rw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotPath, gotKey, gotQuery, gotBody = r.URL.Path, r.Header.Get("X-Internal-Key"), r.URL.RawQuery, string(b)
		if r.Method == "POST" {
			w.WriteHeader(402)
			w.Write([]byte(`{"code":"insufficient","balance":5}`))
			return
		}
		w.Write([]byte(`{"balance":9,"items":[]}`))
	}))
	defer rw.Close()
	srv := &Server{Store: &stub{}, Key: "k", Rewards: &Rewards{Base: rw.URL, Key: "ev"}}
	if c := do(t, srv, "GET", "/v1/shop/switch/game-items?subject=abc", "k", "").Code; c != 200 || gotPath != "/internal/v1/adapter/shop" || gotKey != "ev" || !strings.Contains(gotQuery, "namespace=switch") {
		t.Fatalf("list: %d %s %s %s", c, gotPath, gotKey, gotQuery)
	}
	if c := do(t, srv, "GET", "/v1/shop/switch/game-items", "k", "").Code; c != 400 {
		t.Fatalf("no subject: %d", c)
	}
	rec := do(t, srv, "POST", "/v1/shop/wiiu/game-items/redeem", "k", `{"subject":"1","game":"dst","game_ref":"pack_x","trans_id":"t"}`)
	if rec.Code != 402 || !strings.Contains(gotBody, `"namespace":"wiiu"`) {
		t.Fatalf("redeem: %d %s", rec.Code, gotBody)
	}
	if c := do(t, &Server{Store: &stub{}, Key: "k"}, "GET", "/v1/shop/switch/game-items?subject=a", "k", "").Code; c != 503 {
		t.Fatalf("unconfigured: %d", c)
	}
	if c := do(t, srv, "GET", "/v1/shop/switch/game-items?subject=a", "", "").Code; c != 401 {
		t.Fatalf("auth: %d", c)
	}
}
