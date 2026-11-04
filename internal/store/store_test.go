package store

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"
)

func open(t *testing.T) *Store {
	t.Helper()
	url := os.Getenv("SHOP_CORE_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("SHOP_CORE_TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	s, err := Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	for _, q := range []string{"DELETE FROM shop_broken", "DELETE FROM shop_media", "DELETE FROM shop_version",
		"DELETE FROM shop_source_state", "DELETE FROM shop_title", "DELETE FROM shop_sync_run", "DELETE FROM shop_audit"} {
		if _, err := s.pool.Exec(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	return s
}

func TestWiiULifecycle(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	id := "00050000fe000001"
	if err := s.Put(ctx, Title{Console: "wiiu", TitleID: id, Name: "Gamepad Tester", Publisher: "v", Licence: "Apache-2.0", SourceRef: "u"}, "t"); err != nil {
		t.Fatal(err)
	}
	if err := s.PutVersion(ctx, "wiiu", id, Version{Version: 0, VersionLabel: "0.1a"}, "t"); err != nil {
		t.Fatal(err)
	}
	if err := s.PutVersion(ctx, "wiiu", id, Version{Version: 1, VersionLabel: "0.2"}, "t"); err != nil {
		t.Fatal(err)
	}
	if err := s.PutVersion(ctx, "wiiu", "00050000fe0000ff", Version{}, "t"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("version for unknown title: %v", err)
	}
	if err := s.SetPublished(ctx, "wiiu", id, true, "t"); err != nil {
		t.Fatal(err)
	}
	if err := s.Retire(ctx, "wiiu", id, "upstream gone", "t"); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(ctx, "wiiu", id)
	if got.Published || got.RetiredAt == nil || len(got.Versions) != 2 || got.Versions[0].VersionLabel != "0.2" {
		t.Fatalf("retired title: %+v", got)
	}
	if err := s.SetPublished(ctx, "wiiu", id, true, "t"); !errors.Is(err, ErrRetired) {
		t.Fatalf("publish while retired: %v", err)
	}
	if err := s.Unretire(ctx, "wiiu", id, "t"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetPublished(ctx, "wiiu", id, true, "t"); err != nil {
		t.Fatal(err)
	}
	list, _ := s.List(ctx, "wiiu", Filter{})
	if len(list) != 1 || list[0].Latest == nil || list[0].Latest.Version != 1 {
		t.Fatalf("list: %+v", list)
	}
}

func TestSwitchImportIsReadOnlyToWriters(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	items := []Imported{{Title: Title{Console: "switch", TitleID: "01fe000000000000", Name: "OpenPak", Published: true, SourceKind: "nx-shop"},
		Version: &Version{Version: 65536, VersionLabel: "1.0.1"}}}
	a, u, err := s.ApplyImport(ctx, "switch", items)
	if err != nil || a != 1 || u != 0 {
		t.Fatalf("first import: %d %d %v", a, u, err)
	}
	a, u, err = s.ApplyImport(ctx, "switch", items)
	if err != nil || a != 0 || u != 1 {
		t.Fatalf("second import: %d %d %v", a, u, err)
	}
	if err := s.Put(ctx, Title{Console: "switch", TitleID: "01fe000000000001", Name: "x"}, "t"); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("put on switch: %v", err)
	}
	if err := s.Retire(ctx, "switch", "01fe000000000000", "r", "t"); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("retire on switch: %v", err)
	}
}

func TestBrokenReplace(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	since := time.Now().Add(-48 * time.Hour).UTC().Truncate(time.Second)
	if err := s.SetBroken(ctx, "switch", map[string]BrokenEntry{"a": {Since: since, Context: "x"}, "b": {Since: since, Alerted: true}}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetBroken(ctx, "switch", map[string]BrokenEntry{"b": {Since: since, Alerted: true, Context: "y"}}); err != nil {
		t.Fatal(err)
	}
	got, err := s.Broken(ctx, "switch")
	if err != nil || len(got) != 1 || !got["b"].Alerted || !got["b"].Since.Equal(since) || got["b"].Context != "y" {
		t.Fatalf("broken: %+v %v", got, err)
	}
	if _, err := s.Broken(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown console: %v", err)
	}
}

func TestPS3ConsoleRule(t *testing.T) {
	ctx := context.Background()
	s := open(t)
	f, fold, err := s.IDRule(ctx, "ps3")
	if err != nil || f != "^OPHB[0-9]{5}$" || fold != "upper" {
		t.Fatalf("ps3 rule %q %q %v", f, fold, err)
	}
	if f, fold, _ = s.IDRule(ctx, "wiiu"); f != "^[0-9a-f]{16}$" || fold != "lower" {
		t.Fatalf("wiiu rule %q %q", f, fold)
	}
	if _, _, err = s.IDRule(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown console: %v", err)
	}
	if err := s.Put(ctx, Title{Console: "ps3", TitleID: "OPHB00001", Name: "PS3LoadX", Publisher: "bucanero", Licence: "MIT", SourceRef: "https://github.com/bucanero/PS3LoadX"}, "t"); err != nil {
		t.Fatal(err)
	}
}
