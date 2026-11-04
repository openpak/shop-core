package nximport

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchMapsCatalog(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/store/titles" || r.Method != http.MethodGet || r.Header.Get("X-Internal-Key") != "k" {
			http.Error(w, "no", http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"count":2,"titles":[
 {"title_id":"01fe000000000000","published":true,"publisher":"OpenPak","category":"tool","version":65536,"version_label":"1.0.1",
  "updated_at":1790000000,"size_bytes":1234,"distribution":{"source":"https://github.com/x/y","permission":"GPL-3.0, from x/y"},
  "localized":{"ja":{"name":"ja name"},"en-US":{"name":"My App","description":"d"}},"default_lang":"ja"},
 {"title_id":"01aa000000000000","published":false,"localized":{"ja":{"name":"only ja"}},"default_lang":"fr"},
 {"title_id":""}]}`))
	}))
	defer ts.Close()
	items, err := (&Client{Base: ts.URL, Key: "k"}).Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("items: %d", len(items))
	}
	a := items[0]
	if a.Title.Console != "switch" || a.Title.Name != "My App" || a.Title.Licence != "GPL-3.0, from x/y" || !a.Title.Published ||
		a.Version.Version != 65536 || a.Version.RevisedAt == nil || a.Title.SourceRef != "https://github.com/x/y" {
		t.Fatalf("mapped: %+v %+v", a.Title, a.Version)
	}
	if items[1].Title.Name != "only ja" || items[1].Title.Published {
		t.Fatalf("fallback name: %+v", items[1].Title)
	}
	if _, err := (&Client{Base: ts.URL, Key: "wrong"}).Fetch(context.Background()); err == nil {
		t.Fatal("wrong key must fail")
	}
}
