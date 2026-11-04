# Changelog: shop-core

## v0.2.0 - 2026-11-04

- Per-console title id format (card #130, D4): `shop_console.id_format` and `id_case` (schema step 2,
  additive). `switch` and `wiiu` keep 16 lowercase hex digits; new console `ps3` takes `OPHB#####`,
  stored uppercase. The API validates and folds `{id}` by the console's row instead of a fixed 16-hex
  rule; `GET /v1/shop/consoles` returns both fields.

## v0.1.0 - 2026-11-04

- First release (card openpak/prds#124, phase P2). Postgres schema (console dimension), admin API
  for titles, versions, media, publish and retire, the durable broken-package tracker, the sync-run
  ledger, pause, and a read-only Switch mirror from nx-shop's store admin API.
