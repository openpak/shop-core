// Package api is shop-core's internal HTTP surface. Every route except /healthz needs the
// X-Internal-Key header. Errors are JSON {"error": code, "message": text}.
package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/openpak/shop-core/internal/store"
)

// Store is what the API needs; *store.Store implements it.
type Store interface {
	Ping(ctx context.Context) error
	Consoles(ctx context.Context) ([]store.Console, error)
	IDRule(ctx context.Context, console string) (format, fold string, err error)
	SetPaused(ctx context.Context, console string, paused bool, actor string) error
	List(ctx context.Context, console string, f store.Filter) ([]store.Title, error)
	Get(ctx context.Context, console, id string) (store.Title, error)
	Put(ctx context.Context, t store.Title, actor string) error
	PutVersion(ctx context.Context, console, id string, v store.Version, actor string) error
	PutMedia(ctx context.Context, console, id string, media []store.Media, actor string) error
	SetPublished(ctx context.Context, console, id string, published bool, actor string) error
	Retire(ctx context.Context, console, id, reason, actor string) error
	Unretire(ctx context.Context, console, id, actor string) error
	Broken(ctx context.Context, console string) (map[string]store.BrokenEntry, error)
	SetBroken(ctx context.Context, console string, set map[string]store.BrokenEntry) error
	StartRun(ctx context.Context, console, step, runner string) (store.Run, error)
	FinishRun(ctx context.Context, console string, id int64, state, summary string) error
	Runs(ctx context.Context, console string, limit int) ([]store.Run, error)
}

// Importer refreshes a read-only console from its source. Nil means none configured.
type Importer interface {
	Run(ctx context.Context) (added, updated int, err error)
}

type Server struct {
	Store    Store
	Key      string
	Importer Importer // Switch catalog mirror
	Rewards  *Rewards // the points ledger, for game items; nil = not configured
}

var (
	consoleRe = regexp.MustCompile(`^[a-z0-9-]{2,16}$`)
)

func (s *Server) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /healthz", s.healthz)
	h := func(f http.HandlerFunc) http.HandlerFunc { return s.auth(f) }
	mux.HandleFunc("GET /v1/shop/consoles", h(s.consoles))
	mux.HandleFunc("GET /v1/shop/{console}/titles", h(s.list))
	mux.HandleFunc("GET /v1/shop/{console}/titles/{id}", h(s.get))
	mux.HandleFunc("PUT /v1/shop/{console}/titles/{id}", h(s.put))
	mux.HandleFunc("PUT /v1/shop/{console}/titles/{id}/versions/{version}", h(s.putVersion))
	mux.HandleFunc("PUT /v1/shop/{console}/titles/{id}/media", h(s.putMedia))
	mux.HandleFunc("POST /v1/shop/{console}/titles/{id}/publish", h(s.setPublished(true)))
	mux.HandleFunc("POST /v1/shop/{console}/titles/{id}/unpublish", h(s.setPublished(false)))
	mux.HandleFunc("POST /v1/shop/{console}/titles/{id}/retire", h(s.retire))
	mux.HandleFunc("POST /v1/shop/{console}/titles/{id}/unretire", h(s.unretire))
	mux.HandleFunc("GET /v1/shop/{console}/broken", h(s.getBroken))
	mux.HandleFunc("PUT /v1/shop/{console}/broken", h(s.putBroken))
	mux.HandleFunc("POST /v1/shop/{console}/pause", h(s.pause(true)))
	mux.HandleFunc("POST /v1/shop/{console}/resume", h(s.pause(false)))
	mux.HandleFunc("GET /v1/shop/{console}/runs", h(s.runs))
	mux.HandleFunc("POST /v1/shop/{console}/runs", h(s.startRun))
	mux.HandleFunc("POST /v1/shop/{console}/runs/{run}/finish", h(s.finishRun))
	mux.HandleFunc("GET /v1/shop/{console}/game-items", h(s.gameItems))
	mux.HandleFunc("POST /v1/shop/{console}/game-items/redeem", h(s.redeemGameItem))
	mux.HandleFunc("POST /v1/shop/switch/import", h(s.importNow))
}

func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.Key == "" {
			fail(w, http.StatusServiceUnavailable, "internal_key_not_configured", "SHOP_CORE_INTERNAL_KEY is not set")
			return
		}
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Internal-Key")), []byte(s.Key)) != 1 {
			fail(w, http.StatusUnauthorized, "unauthorized", "wrong or missing X-Internal-Key")
			return
		}
		next(w, r)
	}
}

func (s *Server) healthz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	if err := s.Store.Ping(ctx); err != nil {
		fail(w, http.StatusServiceUnavailable, "database", "database unreachable")
		return
	}
	reply(w, http.StatusOK, map[string]string{"status": "ok"})
}

func actor(r *http.Request) string {
	a := r.Header.Get("X-Shop-Actor")
	if len(a) > 80 {
		a = a[:80]
	}
	return a
}

func fail(w http.ResponseWriter, status int, code, msg string) {
	reply(w, status, map[string]string{"error": code, "message": msg})
}

func reply(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<20)).Decode(v); err != nil {
		fail(w, http.StatusBadRequest, "invalid_json", err.Error())
		return false
	}
	return true
}

// storeErr maps store errors to HTTP.
func storeErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		fail(w, http.StatusNotFound, "not_found", "no such console, title or run")
	case errors.Is(err, store.ErrReadOnly):
		fail(w, http.StatusConflict, "read_only", "this console's catalog is imported read-only")
	case errors.Is(err, store.ErrRetired):
		fail(w, http.StatusConflict, "retired", "title is retired; unretire it first")
	default:
		fail(w, http.StatusInternalServerError, "internal", "internal error")
	}
}

func consoleOf(w http.ResponseWriter, r *http.Request) (string, bool) {
	c := r.PathValue("console")
	if !consoleRe.MatchString(c) {
		fail(w, http.StatusBadRequest, "invalid_console", "console must be 2-16 chars of a-z 0-9 -")
		return "", false
	}
	return c, true
}

func (s *Server) titleOf(w http.ResponseWriter, r *http.Request) (console, id string, ok bool) {
	if console, ok = consoleOf(w, r); !ok {
		return
	}
	id = r.PathValue("id")
	format, fold, err := s.Store.IDRule(r.Context(), console)
	if err != nil {
		storeErr(w, err)
		return "", "", false
	}
	// Consoles differ: Switch/Wii U 16 hex digits, ps3 OPHB#####. The rule is the console's row.
	// Matching is case-insensitive for input; the stored form follows the console's fold.
	re, err := regexp.Compile("(?i)" + format)
	if err != nil || !re.MatchString(id) {
		fail(w, http.StatusBadRequest, "invalid_title_id", "title id does not match this console's format ("+format+")")
		return "", "", false
	}
	switch fold {
	case "lower":
		id = strings.ToLower(id)
	case "upper":
		id = strings.ToUpper(id)
	}
	return console, id, true
}

func (s *Server) consoles(w http.ResponseWriter, r *http.Request) {
	cs, err := s.Store.Consoles(r.Context())
	if err != nil {
		storeErr(w, err)
		return
	}
	reply(w, http.StatusOK, map[string]any{"consoles": cs})
}

func (s *Server) list(w http.ResponseWriter, r *http.Request) {
	console, ok := consoleOf(w, r)
	if !ok {
		return
	}
	var f store.Filter
	for key, dst := range map[string]**bool{"published": &f.Published, "retired": &f.Retired} {
		if v := r.URL.Query().Get(key); v != "" {
			b, err := strconv.ParseBool(v)
			if err != nil {
				fail(w, http.StatusBadRequest, "invalid_field", key+" must be true or false")
				return
			}
			*dst = &b
		}
	}
	ts, err := s.Store.List(r.Context(), console, f)
	if err != nil {
		storeErr(w, err)
		return
	}
	reply(w, http.StatusOK, map[string]any{"console": console, "count": len(ts), "titles": ts})
}

func (s *Server) get(w http.ResponseWriter, r *http.Request) {
	console, id, ok := s.titleOf(w, r)
	if !ok {
		return
	}
	t, err := s.Store.Get(r.Context(), console, id)
	if err != nil {
		storeErr(w, err)
		return
	}
	reply(w, http.StatusOK, t)
}

type putBody struct {
	Name        string          `json:"name"`
	Publisher   string          `json:"publisher"`
	Category    string          `json:"category"`
	Licence     string          `json:"licence"`
	SourceKind  string          `json:"source_kind"`
	SourceRef   string          `json:"source_ref"`
	Description string          `json:"description"`
	Meta        json.RawMessage `json:"meta"`
}

func (s *Server) put(w http.ResponseWriter, r *http.Request) {
	console, id, ok := s.titleOf(w, r)
	if !ok {
		return
	}
	var b putBody
	if !decode(w, r, &b) {
		return
	}
	if b.Name == "" || b.Publisher == "" || b.Licence == "" || b.SourceRef == "" {
		fail(w, http.StatusBadRequest, "invalid_field", "name, publisher, licence and source_ref are required")
		return
	}
	t := store.Title{Console: console, TitleID: id, Name: b.Name, Publisher: b.Publisher, Category: b.Category,
		Licence: b.Licence, SourceKind: b.SourceKind, SourceRef: b.SourceRef, Description: b.Description, Meta: b.Meta}
	if err := s.Store.Put(r.Context(), t, actor(r)); err != nil {
		storeErr(w, err)
		return
	}
	got, err := s.Store.Get(r.Context(), console, id)
	if err != nil {
		storeErr(w, err)
		return
	}
	reply(w, http.StatusOK, got)
}

func (s *Server) putVersion(w http.ResponseWriter, r *http.Request) {
	console, id, ok := s.titleOf(w, r)
	if !ok {
		return
	}
	n, err := strconv.ParseInt(r.PathValue("version"), 10, 64)
	if err != nil || n < 0 {
		fail(w, http.StatusBadRequest, "invalid_field", "version must be a non-negative integer")
		return
	}
	var v store.Version
	if !decode(w, r, &v) {
		return
	}
	v.Version = n
	if err := s.Store.PutVersion(r.Context(), console, id, v, actor(r)); err != nil {
		storeErr(w, err)
		return
	}
	reply(w, http.StatusOK, v)
}

func (s *Server) putMedia(w http.ResponseWriter, r *http.Request) {
	console, id, ok := s.titleOf(w, r)
	if !ok {
		return
	}
	var body struct {
		Media []store.Media `json:"media"`
	}
	if !decode(w, r, &body) {
		return
	}
	for _, m := range body.Media {
		if m.Kind == "" || m.Ref == "" {
			fail(w, http.StatusBadRequest, "invalid_field", "every media item needs kind and ref")
			return
		}
	}
	if err := s.Store.PutMedia(r.Context(), console, id, body.Media, actor(r)); err != nil {
		storeErr(w, err)
		return
	}
	reply(w, http.StatusOK, map[string]int{"media": len(body.Media)})
}

func (s *Server) setPublished(published bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		console, id, ok := s.titleOf(w, r)
		if !ok {
			return
		}
		if err := s.Store.SetPublished(r.Context(), console, id, published, actor(r)); err != nil {
			storeErr(w, err)
			return
		}
		reply(w, http.StatusOK, map[string]any{"title_id": id, "published": published})
	}
}

func (s *Server) retire(w http.ResponseWriter, r *http.Request) {
	console, id, ok := s.titleOf(w, r)
	if !ok {
		return
	}
	var b struct {
		Reason string `json:"reason"`
	}
	if r.ContentLength != 0 && !decode(w, r, &b) {
		return
	}
	if b.Reason == "" {
		b.Reason = "retired by staff"
	}
	if err := s.Store.Retire(r.Context(), console, id, b.Reason, actor(r)); err != nil {
		storeErr(w, err)
		return
	}
	reply(w, http.StatusOK, map[string]any{"title_id": id, "retired": true, "reason": b.Reason})
}

func (s *Server) unretire(w http.ResponseWriter, r *http.Request) {
	console, id, ok := s.titleOf(w, r)
	if !ok {
		return
	}
	if err := s.Store.Unretire(r.Context(), console, id, actor(r)); err != nil {
		storeErr(w, err)
		return
	}
	reply(w, http.StatusOK, map[string]any{"title_id": id, "retired": false})
}

func (s *Server) getBroken(w http.ResponseWriter, r *http.Request) {
	console, ok := consoleOf(w, r)
	if !ok {
		return
	}
	set, err := s.Store.Broken(r.Context(), console)
	if err != nil {
		storeErr(w, err)
		return
	}
	reply(w, http.StatusOK, map[string]any{"console": console, "broken": set})
}

func (s *Server) putBroken(w http.ResponseWriter, r *http.Request) {
	console, ok := consoleOf(w, r)
	if !ok {
		return
	}
	var body struct {
		Broken map[string]store.BrokenEntry `json:"broken"`
	}
	if !decode(w, r, &body) {
		return
	}
	if err := s.Store.SetBroken(r.Context(), console, body.Broken); err != nil {
		storeErr(w, err)
		return
	}
	reply(w, http.StatusOK, map[string]int{"broken": len(body.Broken)})
}

func (s *Server) pause(paused bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		console, ok := consoleOf(w, r)
		if !ok {
			return
		}
		if err := s.Store.SetPaused(r.Context(), console, paused, actor(r)); err != nil {
			storeErr(w, err)
			return
		}
		reply(w, http.StatusOK, map[string]any{"console": console, "paused": paused})
	}
}

func (s *Server) runs(w http.ResponseWriter, r *http.Request) {
	console, ok := consoleOf(w, r)
	if !ok {
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	rs, err := s.Store.Runs(r.Context(), console, limit)
	if err != nil {
		storeErr(w, err)
		return
	}
	reply(w, http.StatusOK, map[string]any{"console": console, "runs": rs})
}

func (s *Server) startRun(w http.ResponseWriter, r *http.Request) {
	console, ok := consoleOf(w, r)
	if !ok {
		return
	}
	var b struct{ Step, Runner string }
	if r.ContentLength != 0 && !decode(w, r, &b) {
		return
	}
	run, err := s.Store.StartRun(r.Context(), console, b.Step, b.Runner)
	if err != nil {
		storeErr(w, err)
		return
	}
	reply(w, http.StatusCreated, run)
}

func (s *Server) finishRun(w http.ResponseWriter, r *http.Request) {
	console, ok := consoleOf(w, r)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("run"), 10, 64)
	if err != nil {
		fail(w, http.StatusBadRequest, "invalid_field", "run id must be an integer")
		return
	}
	var b struct{ State, Summary string }
	if !decode(w, r, &b) {
		return
	}
	if b.State != "ok" && b.State != "failed" {
		fail(w, http.StatusBadRequest, "invalid_field", "state must be ok or failed")
		return
	}
	if err := s.Store.FinishRun(r.Context(), console, id, b.State, b.Summary); err != nil {
		storeErr(w, err)
		return
	}
	reply(w, http.StatusOK, map[string]string{"state": b.State})
}

func (s *Server) importNow(w http.ResponseWriter, r *http.Request) {
	if s.Importer == nil {
		fail(w, http.StatusServiceUnavailable, "not_configured", "no Switch catalog source is configured")
		return
	}
	added, updated, err := s.Importer.Run(r.Context())
	if err != nil {
		fail(w, http.StatusBadGateway, "import_failed", fmt.Sprint(err))
		return
	}
	reply(w, http.StatusOK, map[string]int{"added": added, "updated": updated})
}
