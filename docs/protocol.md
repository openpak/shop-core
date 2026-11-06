# Protocol: shop-core

Internal HTTP on `:20240`, plain, internal network only. Every route except `/healthz` needs
`X-Internal-Key: <SHOP_CORE_INTERNAL_KEY>` (unset key: 503 `internal_key_not_configured`; wrong
key: 401). An optional `X-Shop-Actor` header names who acted; it lands in the audit table.
Errors are `{"error": code, "message": text}`.

`{console}` is `switch`, `wiiu` or `ps3` (2-16 chars of `a-z 0-9 -`; consoles are rows in `shop_console`).
`{id}` is the console's title id. Each console row carries `id_format` (regexp, matched case-insensitively)
and `id_case` (`lower`, `upper` or `none`, how the id is stored): `switch` and `wiiu` take 16 hex digits,
stored lowercase; `ps3` takes `OPHB#####` (store homebrew), stored uppercase. A malformed id is 400
`invalid_title_id`, an unknown console 404 `not_found`. `GET /v1/shop/consoles` shows both fields.

## Consoles

| Console | Catalog | Notes |
| --- | --- | --- |
| `switch` | **read-only mirror** of nx-shop's `GET /internal/store/titles`, every 15 minutes and on `POST /v1/shop/switch/import` | Every title write answers 409 `read_only`. nx-shop stays the source of truth until the Switch migrates (plan phase P7). Broken tracker, pause and runs work. |
| `wiiu` | written through this API | First native console. |
| `ps3` | written through this API | PlayStation 3 homebrew shop (card #130). Title ids `OPHB00001..OPHB99999`. |

`GET /v1/shop/consoles` returns each console with counts of titles, published, retired and broken.

## Titles

| Route | Purpose |
| --- | --- |
| `GET /v1/shop/{console}/titles?published=&retired=` | list, with the newest version per title |
| `GET /v1/shop/{console}/titles/{id}` | one title with all versions and media |
| `PUT /v1/shop/{console}/titles/{id}` | create or update metadata: `name`, `publisher`, `licence`, `source_ref` required; `category`, `source_kind`, `description`, `meta` optional. Does not change published or retired state. |
| `PUT .../titles/{id}/versions/{n}` | create or update version `n`: `version_label`, `artifact_ref`, `size`, `built_at`, `changelog`, `revised_at` |
| `PUT .../titles/{id}/media` | replace media: `{"media":[{"kind":"icon|screenshot","position":0,"ref":"..."}]}` |
| `POST .../publish`, `.../unpublish` | show or hide in the storefront. New titles start unpublished. A retired title cannot be published (409 `retired`). |
| `POST .../retire` `{"reason": "..."}` | hide from new buyers: unpublished, `retired_at` set. Nothing is deleted; **current owners keep their download** (the adapter serves retired titles to owners). Reversible. |
| `POST .../unretire` | clear the mark; the title stays unpublished until published again |

Retire policy (decision D7): a title whose upstream is missing for 3 consecutive syncs is
retired by the update loop (`shop_source_state.strikes`), staff are told, and `unretire` undoes it.

## Broken tracker

`GET /v1/shop/{console}/broken` returns `{"broken": {package: {since, alerted, context}}}`.
`PUT` replaces the set in one transaction. Pakka owns the semantics (first-seen, grace period,
alerted, cleared) and persists its state here.

## Sync runs

`GET /v1/shop/{console}/runs?limit=`, `POST .../runs` `{"step":"sync","runner":"..."}` starts a run
(201, returns `id`), `POST .../runs/{run}/finish` `{"state":"ok|failed","summary":"..."}` ends it.
Pause: `POST /v1/shop/{console}/pause`, `.../resume`. The `shop_job` table is reserved for the
Pakka scheduler (plan section 7).

## Game items (points only)

`GET /v1/shop/{console}/game-items?subject=<console identity>[&game=<id>]` lists the game cosmetics a player can
unlock for Rewards points (`game`, `game_ref`, `slot`, `name`, `price` in points, `owned`) with the player's points
(`balance`). `POST /v1/shop/{console}/game-items/redeem` `{subject, game, game_ref, expected_price?, trans_id?}`
unlocks one: 200 `{balance, game_ref, item}`, 402 `insufficient`, 409 `price_mismatch|owned|not_for_sale`, 404 identity
not linked. Both relay to the Rewards ledger (`SHOP_CORE_REWARDS_URL`, `SHOP_CORE_REWARDS_KEY`: the events key); the ledger is
the only place points move, so the DST in-game shop and every console shop share it. Unset: 503 `rewards_not_configured`.
The player's identity is the console's own (Switch: BAAS user id; namespace = console).

`GET /v1/shop/{console}/game-items/{item}/art` returns the item's preview image (the list's `item` and `art` fields say whether there is one); 404 without.

## Health

`GET /healthz` returns 200 `{"status":"ok"}` when the database answers.

## Tables

`shop_console`, `shop_title`, `shop_version`, `shop_media`, `shop_source_state`, `shop_broken`,
`shop_sync_run`, `shop_job`, `shop_audit`. See `internal/store/schema.go`.
