package store

// schema is applied at start, one numbered step at a time; a step runs once (schema_version).
// Never edit a released step: append a new one.
var schema = []string{
	// 1: the catalog, with the console as a dimension of every row.
	`
CREATE TABLE shop_console (
  console    text PRIMARY KEY,
  name       text NOT NULL,
  read_only  boolean NOT NULL DEFAULT false, -- catalog is imported from another service
  paused     boolean NOT NULL DEFAULT false, -- no sync runs while paused
  created_at timestamptz NOT NULL DEFAULT now()
);
INSERT INTO shop_console (console, name, read_only) VALUES ('switch', 'Switch', true), ('wiiu', 'Wii U', false);

CREATE TABLE shop_title (
  console       text NOT NULL REFERENCES shop_console(console),
  title_id      text NOT NULL,
  name          text NOT NULL DEFAULT '',
  publisher     text NOT NULL DEFAULT '',
  category      text NOT NULL DEFAULT '',
  licence       text NOT NULL DEFAULT '',   -- distribution.permission as recorded by the packager
  source_kind   text NOT NULL DEFAULT 'manual',
  source_ref    text NOT NULL DEFAULT '',   -- upstream URL or repo
  description   text NOT NULL DEFAULT '',
  published     boolean NOT NULL DEFAULT false,
  retired_at    timestamptz,
  retire_reason text NOT NULL DEFAULT '',
  meta          jsonb NOT NULL DEFAULT '{}',
  last_seen_at  timestamptz,                -- import only: last time the source listed it
  created_at    timestamptz NOT NULL DEFAULT now(),
  updated_at    timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (console, title_id)
);

CREATE TABLE shop_version (
  console       text NOT NULL,
  title_id      text NOT NULL,
  version       bigint NOT NULL,
  version_label text NOT NULL DEFAULT '',
  artifact_ref  text NOT NULL DEFAULT '',
  size          bigint NOT NULL DEFAULT 0,
  built_at      timestamptz,
  changelog     text NOT NULL DEFAULT '',
  revised_at    timestamptz,
  PRIMARY KEY (console, title_id, version),
  FOREIGN KEY (console, title_id) REFERENCES shop_title(console, title_id) ON DELETE CASCADE
);

CREATE TABLE shop_media (
  console  text NOT NULL,
  title_id text NOT NULL,
  kind     text NOT NULL,                   -- icon, screenshot
  position int  NOT NULL DEFAULT 0,
  ref      text NOT NULL,
  PRIMARY KEY (console, title_id, kind, position),
  FOREIGN KEY (console, title_id) REFERENCES shop_title(console, title_id) ON DELETE CASCADE
);

CREATE TABLE shop_source_state (
  console          text NOT NULL,
  title_id         text NOT NULL,
  upstream_tag     text NOT NULL DEFAULT '',
  upstream_seen_at timestamptz,
  strikes          int  NOT NULL DEFAULT 0,  -- consecutive syncs the upstream was missing
  PRIMARY KEY (console, title_id),
  FOREIGN KEY (console, title_id) REFERENCES shop_title(console, title_id) ON DELETE CASCADE
);

CREATE TABLE shop_broken (
  console    text NOT NULL REFERENCES shop_console(console),
  package    text NOT NULL,
  context    text NOT NULL DEFAULT '',
  since      timestamptz NOT NULL,
  alerted    boolean NOT NULL DEFAULT false,
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (console, package)
);

CREATE TABLE shop_sync_run (
  id      bigserial PRIMARY KEY,
  console text NOT NULL REFERENCES shop_console(console),
  step    text NOT NULL DEFAULT 'sync',
  state   text NOT NULL DEFAULT 'running',   -- running, ok, failed
  started timestamptz NOT NULL DEFAULT now(),
  ended   timestamptz,
  summary text NOT NULL DEFAULT '',
  runner  text NOT NULL DEFAULT ''
);
CREATE INDEX shop_sync_run_console ON shop_sync_run (console, started DESC);

CREATE TABLE shop_job (
  id          bigserial PRIMARY KEY,
  console     text NOT NULL REFERENCES shop_console(console),
  kind        text NOT NULL,
  state       text NOT NULL DEFAULT 'queued',
  lease_until timestamptz,
  runner      text NOT NULL DEFAULT '',
  payload     jsonb NOT NULL DEFAULT '{}',
  created_at  timestamptz NOT NULL DEFAULT now(),
  updated_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE shop_audit (
  id      bigserial PRIMARY KEY,
  at      timestamptz NOT NULL DEFAULT now(),
  console text NOT NULL,
  actor   text NOT NULL DEFAULT '',
  action  text NOT NULL,
  target  text NOT NULL DEFAULT '',
  detail  text NOT NULL DEFAULT ''
);
`,
	// 2: per-console title id format (card #130). Existing consoles keep 16 lowercase hex digits; ps3 uses
	// the 9-character PlayStation title id, OPHB##### for store homebrew. id_case folds the stored id.
	`
ALTER TABLE shop_console
  ADD COLUMN id_format text NOT NULL DEFAULT '^[0-9a-f]{16}$',
  ADD COLUMN id_case   text NOT NULL DEFAULT 'lower' CHECK (id_case IN ('lower', 'upper', 'none'));
INSERT INTO shop_console (console, name, read_only, id_format, id_case)
  VALUES ('ps3', 'PlayStation 3', false, '^OPHB[0-9]{5}$', 'upper');
`,
}
