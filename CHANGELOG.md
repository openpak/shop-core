# Changelog: shop-core

## v0.3.1 - 2026-11-06

- Card #148: `GET /v1/shop/{console}/game-items/{item}/art` relays an item's preview picture from the ledger, so a console shop shows each pack's own art.

## v0.3.0 - 2026-11-06

- Card #148: game items for Rewards points. `GET /v1/shop/{console}/game-items` and `POST .../game-items/redeem` relay to the Rewards ledger (events key), so every console shop lists and unlocks game cosmetics through the one ledger. Points only. Needs `SHOP_CORE_REWARDS_URL` and `SHOP_CORE_REWARDS_KEY`.

## v0.2.0 - 2026-11-04

- Per-console title id format (card #130, D4): `shop_console.id_format` and `id_case` (schema step 2,
  additive). `switch` and `wiiu` keep 16 lowercase hex digits; new console `ps3` takes `OPHB#####`,
  stored uppercase. The API validates and folds `{id}` by the console's row instead of a fixed 16-hex
  rule; `GET /v1/shop/consoles` returns both fields.

## v0.1.0 - 2026-11-04

- First release (card openpak/prds#124, phase P2). Postgres schema (console dimension), admin API
  for titles, versions, media, publish and retire, the durable broken-package tracker, the sync-run
  ledger, pause, and a read-only Switch mirror from nx-shop's store admin API.
