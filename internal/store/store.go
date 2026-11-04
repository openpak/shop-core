// Package store is the shop catalog: titles, versions, media, upstream state, the broken-package
// tracker and the sync-run ledger, in one Postgres database, with the console as a dimension of
// every row. Nothing here is console-shaped; adapters (Wii U samurai/ninja, Switch nx-shop) map
// their wire formats to and from these rows.
package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrNotFound = errors.New("shop-core: not found")
	ErrReadOnly = errors.New("shop-core: console catalog is read-only")
	ErrRetired  = errors.New("shop-core: title is retired; unretire it before publishing")
)

type Store struct{ pool *pgxpool.Pool }

// Open connects and applies the schema.
func Open(ctx context.Context, url string) (*Store, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	s := &Store{pool: pool}
	if err := s.migrate(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close()                         { s.pool.Close() }
func (s *Store) Ping(ctx context.Context) error { return s.pool.Ping(ctx) }

func (s *Store) migrate(ctx context.Context) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	// One migrator at a time (two replicas starting together).
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(724001)`); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_version (n int PRIMARY KEY, at timestamptz NOT NULL DEFAULT now())`); err != nil {
		return err
	}
	var have int
	if err := tx.QueryRow(ctx, `SELECT COALESCE(max(n), 0) FROM schema_version`).Scan(&have); err != nil {
		return err
	}
	for i := have; i < len(schema); i++ {
		if _, err := tx.Exec(ctx, schema[i]); err != nil {
			return fmt.Errorf("schema step %d: %w", i+1, err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO schema_version (n) VALUES ($1)`, i+1); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// Console is one shop network.
type Console struct {
	Console   string `json:"console"`
	Name      string `json:"name"`
	ReadOnly  bool   `json:"read_only"`
	Paused    bool   `json:"paused"`
	Titles    int    `json:"titles"`
	Published int    `json:"published"`
	Retired   int    `json:"retired"`
	Broken    int    `json:"broken"`
	IDFormat  string `json:"id_format"`
	IDCase    string `json:"id_case"`
}

func (s *Store) Consoles(ctx context.Context) ([]Console, error) {
	rows, err := s.pool.Query(ctx, `
SELECT c.console, c.name, c.read_only, c.paused, c.id_format, c.id_case,
  (SELECT count(*) FROM shop_title t WHERE t.console = c.console),
  (SELECT count(*) FROM shop_title t WHERE t.console = c.console AND t.published),
  (SELECT count(*) FROM shop_title t WHERE t.console = c.console AND t.retired_at IS NOT NULL),
  (SELECT count(*) FROM shop_broken b WHERE b.console = c.console)
FROM shop_console c ORDER BY c.console`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Console{}
	for rows.Next() {
		var c Console
		if err := rows.Scan(&c.Console, &c.Name, &c.ReadOnly, &c.Paused, &c.IDFormat, &c.IDCase, &c.Titles, &c.Published, &c.Retired, &c.Broken); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// IDRule returns the console's title id pattern and case folding ("lower", "upper" or "none").
func (s *Store) IDRule(ctx context.Context, console string) (format, fold string, err error) {
	err = s.pool.QueryRow(ctx, `SELECT id_format, id_case FROM shop_console WHERE console = $1`, console).Scan(&format, &fold)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", ErrNotFound
	}
	return format, fold, err
}

func (s *Store) Console(ctx context.Context, console string) (Console, error) {
	all, err := s.Consoles(ctx)
	if err != nil {
		return Console{}, err
	}
	for _, c := range all {
		if c.Console == console {
			return c, nil
		}
	}
	return Console{}, ErrNotFound
}

func (s *Store) SetPaused(ctx context.Context, console string, paused bool, actor string) error {
	tag, err := s.pool.Exec(ctx, `UPDATE shop_console SET paused = $2 WHERE console = $1`, console, paused)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	action := "resume"
	if paused {
		action = "pause"
	}
	return s.Audit(ctx, console, actor, action, "", "")
}

// Title is one catalog row. Versions and Media are filled by Get only.
type Title struct {
	Console      string          `json:"console"`
	TitleID      string          `json:"title_id"`
	Name         string          `json:"name"`
	Publisher    string          `json:"publisher"`
	Category     string          `json:"category"`
	Licence      string          `json:"licence"`
	SourceKind   string          `json:"source_kind"`
	SourceRef    string          `json:"source_ref"`
	Description  string          `json:"description"`
	Published    bool            `json:"published"`
	RetiredAt    *time.Time      `json:"retired_at,omitempty"`
	RetireReason string          `json:"retire_reason,omitempty"`
	Meta         json.RawMessage `json:"meta,omitempty"`
	LastSeenAt   *time.Time      `json:"last_seen_at,omitempty"`
	UpdatedAt    time.Time       `json:"updated_at"`
	Latest       *Version        `json:"latest,omitempty"`
	Versions     []Version       `json:"versions,omitempty"`
	Media        []Media         `json:"media,omitempty"`
}

type Version struct {
	Version      int64      `json:"version"`
	VersionLabel string     `json:"version_label"`
	ArtifactRef  string     `json:"artifact_ref"`
	Size         int64      `json:"size"`
	BuiltAt      *time.Time `json:"built_at,omitempty"`
	Changelog    string     `json:"changelog"`
	RevisedAt    *time.Time `json:"revised_at,omitempty"`
}

type Media struct {
	Kind     string `json:"kind"`
	Position int    `json:"position"`
	Ref      string `json:"ref"`
}

// Filter narrows List. Nil means any.
type Filter struct {
	Published *bool
	Retired   *bool
}

const titleCols = `console, title_id, name, publisher, category, licence, source_kind, source_ref, description,
 published, retired_at, retire_reason, meta, last_seen_at, updated_at`

func scanTitle(row pgx.Row) (Title, error) {
	var t Title
	var meta []byte
	err := row.Scan(&t.Console, &t.TitleID, &t.Name, &t.Publisher, &t.Category, &t.Licence, &t.SourceKind, &t.SourceRef,
		&t.Description, &t.Published, &t.RetiredAt, &t.RetireReason, &meta, &t.LastSeenAt, &t.UpdatedAt)
	t.Meta = meta
	return t, err
}

func (s *Store) List(ctx context.Context, console string, f Filter) ([]Title, error) {
	if _, err := s.Console(ctx, console); err != nil {
		return nil, err
	}
	q := `SELECT ` + titleCols + ` FROM shop_title WHERE console = $1`
	args := []any{console}
	if f.Published != nil {
		args = append(args, *f.Published)
		q += fmt.Sprintf(" AND published = $%d", len(args))
	}
	if f.Retired != nil {
		if *f.Retired {
			q += " AND retired_at IS NOT NULL"
		} else {
			q += " AND retired_at IS NULL"
		}
	}
	rows, err := s.pool.Query(ctx, q+" ORDER BY title_id", args...)
	if err != nil {
		return nil, err
	}
	out := []Title{}
	for rows.Next() {
		t, err := scanTitle(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, t)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// The newest version of every title in one query.
	vr, err := s.pool.Query(ctx, `SELECT DISTINCT ON (title_id) title_id, version, version_label, artifact_ref, size, built_at, changelog, revised_at
FROM shop_version WHERE console = $1 ORDER BY title_id, version DESC`, console)
	if err != nil {
		return nil, err
	}
	defer vr.Close()
	latest := map[string]Version{}
	for vr.Next() {
		var id string
		var v Version
		if err := vr.Scan(&id, &v.Version, &v.VersionLabel, &v.ArtifactRef, &v.Size, &v.BuiltAt, &v.Changelog, &v.RevisedAt); err != nil {
			return nil, err
		}
		latest[id] = v
	}
	for i := range out {
		if v, ok := latest[out[i].TitleID]; ok {
			v := v
			out[i].Latest = &v
		}
	}
	return out, vr.Err()
}

func (s *Store) Get(ctx context.Context, console, id string) (Title, error) {
	t, err := scanTitle(s.pool.QueryRow(ctx, `SELECT `+titleCols+` FROM shop_title WHERE console = $1 AND title_id = $2`, console, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return t, ErrNotFound
	}
	if err != nil {
		return t, err
	}
	vr, err := s.pool.Query(ctx, `SELECT version, version_label, artifact_ref, size, built_at, changelog, revised_at
FROM shop_version WHERE console = $1 AND title_id = $2 ORDER BY version DESC`, console, id)
	if err != nil {
		return t, err
	}
	for vr.Next() {
		var v Version
		if err := vr.Scan(&v.Version, &v.VersionLabel, &v.ArtifactRef, &v.Size, &v.BuiltAt, &v.Changelog, &v.RevisedAt); err != nil {
			vr.Close()
			return t, err
		}
		t.Versions = append(t.Versions, v)
	}
	vr.Close()
	mr, err := s.pool.Query(ctx, `SELECT kind, position, ref FROM shop_media WHERE console = $1 AND title_id = $2 ORDER BY kind, position`, console, id)
	if err != nil {
		return t, err
	}
	defer mr.Close()
	for mr.Next() {
		var m Media
		if err := mr.Scan(&m.Kind, &m.Position, &m.Ref); err != nil {
			return t, err
		}
		t.Media = append(t.Media, m)
	}
	return t, mr.Err()
}

func (s *Store) writable(ctx context.Context, console string) error {
	var ro bool
	err := s.pool.QueryRow(ctx, `SELECT read_only FROM shop_console WHERE console = $1`, console).Scan(&ro)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if ro {
		return ErrReadOnly
	}
	return nil
}

// Put creates or updates a title's metadata (not its publish or retire state).
func (s *Store) Put(ctx context.Context, t Title, actor string) error {
	if err := s.writable(ctx, t.Console); err != nil {
		return err
	}
	if len(t.Meta) == 0 {
		t.Meta = json.RawMessage(`{}`)
	}
	if t.SourceKind == "" {
		t.SourceKind = "manual"
	}
	_, err := s.pool.Exec(ctx, `
INSERT INTO shop_title (console, title_id, name, publisher, category, licence, source_kind, source_ref, description, meta)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
ON CONFLICT (console, title_id) DO UPDATE SET name = EXCLUDED.name, publisher = EXCLUDED.publisher,
  category = EXCLUDED.category, licence = EXCLUDED.licence, source_kind = EXCLUDED.source_kind,
  source_ref = EXCLUDED.source_ref, description = EXCLUDED.description, meta = EXCLUDED.meta, updated_at = now()`,
		t.Console, t.TitleID, t.Name, t.Publisher, t.Category, t.Licence, t.SourceKind, t.SourceRef, t.Description, []byte(t.Meta))
	if err != nil {
		return err
	}
	return s.Audit(ctx, t.Console, actor, "put", t.TitleID, t.Name)
}

func (s *Store) PutVersion(ctx context.Context, console, id string, v Version, actor string) error {
	if err := s.writable(ctx, console); err != nil {
		return err
	}
	tag, err := s.pool.Exec(ctx, `
INSERT INTO shop_version (console, title_id, version, version_label, artifact_ref, size, built_at, changelog, revised_at)
SELECT $1,$2,$3,$4,$5,$6,$7,$8,COALESCE($9, now()) WHERE EXISTS (SELECT 1 FROM shop_title WHERE console = $1 AND title_id = $2)
ON CONFLICT (console, title_id, version) DO UPDATE SET version_label = EXCLUDED.version_label,
  artifact_ref = EXCLUDED.artifact_ref, size = EXCLUDED.size, built_at = EXCLUDED.built_at,
  changelog = EXCLUDED.changelog, revised_at = EXCLUDED.revised_at`,
		console, id, v.Version, v.VersionLabel, v.ArtifactRef, v.Size, v.BuiltAt, v.Changelog, v.RevisedAt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return s.Audit(ctx, console, actor, "put-version", id, fmt.Sprint(v.Version))
}

func (s *Store) PutMedia(ctx context.Context, console, id string, media []Media, actor string) error {
	if err := s.writable(ctx, console); err != nil {
		return err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var n int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM shop_title WHERE console = $1 AND title_id = $2`, console, id).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	if _, err := tx.Exec(ctx, `DELETE FROM shop_media WHERE console = $1 AND title_id = $2`, console, id); err != nil {
		return err
	}
	for _, m := range media {
		if _, err := tx.Exec(ctx, `INSERT INTO shop_media (console, title_id, kind, position, ref) VALUES ($1,$2,$3,$4,$5)`,
			console, id, m.Kind, m.Position, m.Ref); err != nil {
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	return s.Audit(ctx, console, actor, "put-media", id, fmt.Sprint(len(media)))
}

// SetPublished shows or hides a title in the storefront. Publishing a retired title is refused.
func (s *Store) SetPublished(ctx context.Context, console, id string, published bool, actor string) error {
	if err := s.writable(ctx, console); err != nil {
		return err
	}
	tag, err := s.pool.Exec(ctx, `UPDATE shop_title SET published = $3, updated_at = now()
WHERE console = $1 AND title_id = $2 AND ($3 = false OR retired_at IS NULL)`, console, id, published)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		if _, gerr := s.Get(ctx, console, id); gerr == nil {
			return ErrRetired
		}
		return ErrNotFound
	}
	action := "unpublish"
	if published {
		action = "publish"
	}
	return s.Audit(ctx, console, actor, action, id, "")
}

// Retire hides a title from new buyers. Rows, versions and artifacts stay: current owners keep
// their download (retire is reversible and never deletes).
func (s *Store) Retire(ctx context.Context, console, id, reason, actor string) error {
	if err := s.writable(ctx, console); err != nil {
		return err
	}
	tag, err := s.pool.Exec(ctx, `UPDATE shop_title SET published = false, retired_at = COALESCE(retired_at, now()),
retire_reason = $3, updated_at = now() WHERE console = $1 AND title_id = $2`, console, id, reason)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return s.Audit(ctx, console, actor, "retire", id, reason)
}

// Unretire clears the retire mark. The title stays unpublished until published again.
func (s *Store) Unretire(ctx context.Context, console, id, actor string) error {
	if err := s.writable(ctx, console); err != nil {
		return err
	}
	tag, err := s.pool.Exec(ctx, `UPDATE shop_title SET retired_at = NULL, retire_reason = '', updated_at = now()
WHERE console = $1 AND title_id = $2`, console, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return s.Audit(ctx, console, actor, "unretire", id, "")
}

// BrokenEntry is one package's tracked failure.
type BrokenEntry struct {
	Since   time.Time `json:"since"`
	Alerted bool      `json:"alerted"`
	Context string    `json:"context"`
}

func (s *Store) Broken(ctx context.Context, console string) (map[string]BrokenEntry, error) {
	if _, err := s.Console(ctx, console); err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `SELECT package, since, alerted, context FROM shop_broken WHERE console = $1`, console)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]BrokenEntry{}
	for rows.Next() {
		var p string
		var e BrokenEntry
		if err := rows.Scan(&p, &e.Since, &e.Alerted, &e.Context); err != nil {
			return nil, err
		}
		out[p] = e
	}
	return out, rows.Err()
}

// SetBroken replaces a console's broken set in one transaction (the tracker owns the semantics:
// first-seen, alerted, cleared).
func (s *Store) SetBroken(ctx context.Context, console string, set map[string]BrokenEntry) error {
	if _, err := s.Console(ctx, console); err != nil {
		return err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if _, err := tx.Exec(ctx, `DELETE FROM shop_broken WHERE console = $1`, console); err != nil {
		return err
	}
	for p, e := range set {
		if e.Since.IsZero() {
			e.Since = time.Now()
		}
		if _, err := tx.Exec(ctx, `INSERT INTO shop_broken (console, package, context, since, alerted) VALUES ($1,$2,$3,$4,$5)`,
			console, p, e.Context, e.Since, e.Alerted); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// Run is one sync-run ledger row.
type Run struct {
	ID      int64      `json:"id"`
	Console string     `json:"console"`
	Step    string     `json:"step"`
	State   string     `json:"state"`
	Started time.Time  `json:"started"`
	Ended   *time.Time `json:"ended,omitempty"`
	Summary string     `json:"summary"`
	Runner  string     `json:"runner"`
}

func (s *Store) StartRun(ctx context.Context, console, step, runner string) (Run, error) {
	if step == "" {
		step = "sync"
	}
	r := Run{Console: console, Step: step, State: "running", Runner: runner}
	err := s.pool.QueryRow(ctx, `INSERT INTO shop_sync_run (console, step, runner) VALUES ($1,$2,$3) RETURNING id, started`,
		console, step, runner).Scan(&r.ID, &r.Started)
	return r, err
}

func (s *Store) FinishRun(ctx context.Context, console string, id int64, state, summary string) error {
	tag, err := s.pool.Exec(ctx, `UPDATE shop_sync_run SET state = $3, summary = $4, ended = now() WHERE console = $1 AND id = $2`,
		console, id, state, summary)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) Runs(ctx context.Context, console string, limit int) ([]Run, error) {
	if limit <= 0 || limit > 200 {
		limit = 20
	}
	rows, err := s.pool.Query(ctx, `SELECT id, console, step, state, started, ended, summary, runner FROM shop_sync_run
WHERE console = $1 ORDER BY started DESC LIMIT $2`, console, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Run{}
	for rows.Next() {
		var r Run
		if err := rows.Scan(&r.ID, &r.Console, &r.Step, &r.State, &r.Started, &r.Ended, &r.Summary, &r.Runner); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// Audit records a staff or tool action. Details are short and carry no secrets.
func (s *Store) Audit(ctx context.Context, console, actor, action, target, detail string) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO shop_audit (console, actor, action, target, detail) VALUES ($1,$2,$3,$4,$5)`,
		console, actor, action, target, detail)
	return err
}

// Imported is one title as an importer sees it. Retire and publish state of a read-only console
// come from the source.
type Imported struct {
	Title    Title
	Version  *Version
	Source   string
	SeenAtTS time.Time
}

// ApplyImport upserts a read-only console's catalog from its source of truth. It never deletes:
// a title the source stops listing keeps its row, and last_seen_at shows how stale it is.
func (s *Store) ApplyImport(ctx context.Context, console string, items []Imported) (added, updated int, err error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	for _, it := range items {
		t := it.Title
		if len(t.Meta) == 0 {
			t.Meta = json.RawMessage(`{}`)
		}
		var inserted bool
		err := tx.QueryRow(ctx, `
INSERT INTO shop_title (console, title_id, name, publisher, category, licence, source_kind, source_ref, description, published, meta, last_seen_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,now())
ON CONFLICT (console, title_id) DO UPDATE SET name = EXCLUDED.name, publisher = EXCLUDED.publisher,
  category = EXCLUDED.category, licence = EXCLUDED.licence, source_kind = EXCLUDED.source_kind, source_ref = EXCLUDED.source_ref,
  description = EXCLUDED.description, published = EXCLUDED.published, meta = EXCLUDED.meta, last_seen_at = now(),
  updated_at = CASE WHEN shop_title.name IS DISTINCT FROM EXCLUDED.name OR shop_title.published IS DISTINCT FROM EXCLUDED.published
    OR shop_title.meta IS DISTINCT FROM EXCLUDED.meta THEN now() ELSE shop_title.updated_at END
RETURNING (xmax = 0)`,
			console, t.TitleID, t.Name, t.Publisher, t.Category, t.Licence, t.SourceKind, t.SourceRef, t.Description, t.Published, []byte(t.Meta)).Scan(&inserted)
		if err != nil {
			return 0, 0, err
		}
		if inserted {
			added++
		} else {
			updated++
		}
		if v := it.Version; v != nil {
			if _, err := tx.Exec(ctx, `
INSERT INTO shop_version (console, title_id, version, version_label, size, changelog, revised_at)
VALUES ($1,$2,$3,$4,$5,$6,$7)
ON CONFLICT (console, title_id, version) DO UPDATE SET version_label = EXCLUDED.version_label, size = EXCLUDED.size,
  changelog = EXCLUDED.changelog, revised_at = EXCLUDED.revised_at`,
				console, t.TitleID, v.Version, v.VersionLabel, v.Size, v.Changelog, v.RevisedAt); err != nil {
				return 0, 0, err
			}
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, 0, err
	}
	return added, updated, nil
}
