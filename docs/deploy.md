# Deploy: shop-core

Image `ghcr.io/openpak/shop-core` on tag (`vX.Y.Z`), listening on 20240. Quadlet sample:
`deploy/openpak-shop-core.container`. Needs Postgres database `shop_core` and these variables in
`~/openpak/deploy/.env`:

| Variable | Meaning |
| --- | --- |
| `SHOP_CORE_DATABASE_URL` | Postgres URL of `shop_core` |
| `SHOP_CORE_INTERNAL_KEY` | key every API caller presents |
| `SHOP_CORE_NX_API_BASE`, `SHOP_CORE_NX_KEY` | nx-shop store admin API (`http://openpak-nx-shop:20070`) and its internal key, for the read-only Switch mirror |
| `SHOP_CORE_URL` | on the website and Pakka: `http://openpak-shop-core:20240` |

Callers: Pakka (`PAKKA_SHOP_CORE_URL`, `PAKKA_SHOP_CORE_KEY`), the website `/healthz/shop-core`
(status page), and the packaging runner on the workstation (through the SSH tunnel, as the
Switch sync does). Logs are one line per request on stdout, collected by `openpak-logd`.
