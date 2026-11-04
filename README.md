# shop-core

The console-neutral shop catalog. One Postgres store for every console's shop, with the console
as a dimension of every row: titles, versions, media, upstream state, the broken-package tracker
and the sync-run ledger. Console services (the Wii U samurai/ninja front, nx-shop for the Switch
later) are adapters over it and own their wire formats.

| | |
| --- | --- |
| Network | All consoles (Switch mirrored read-only; Wii U is the first writer) |
| Role | Catalog and update-loop state for the shops; internal admin API for tools, Pakka and the website |
| Depends on | Postgres (database `shop_core`); optional: nx-shop's store admin API for the Switch mirror |
| Port | 20240 HTTP |
| Licence | AGPL-3.0-only |

## Build and run

```sh
go build ./cmd/shop-core
cp example.env .env   # fill in, then export the variables
SHOP_CORE_TEST_DATABASE_URL=postgres://... go test ./...   # store tests need a database, the rest do not
```

The schema migrates at start. Every request is logged as one line (method, path, status,
duration); no query strings, headers or bodies.

## Docs

- [Protocol](docs/protocol.md): the admin API
- [Deploy](docs/deploy.md)

PRDs: openpak/prds (private), `repos/wiiu-eshop-plan.md`.

## Licence

AGPL-3.0-only. See [LICENSE](LICENSE).
