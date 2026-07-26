# mig

[![pipeline](https://github.com/acim/mig/actions/workflows/pipeline.yaml/badge.svg)](https://github.com/acim/mig/actions/workflows/pipeline.yaml)
[![Go Reference](https://pkg.go.dev/badge/go.acim.net/mig.svg)](https://pkg.go.dev/go.acim.net/mig)
[![Go Report](https://goreportcard.com/badge/go.acim.net/mig)](https://goreportcard.com/report/go.acim.net/mig)
![Go Coverage](https://img.shields.io/badge/coverage-96.9%25-brightgreen?style=flat&logo=go)

Go PostgreSQL database schema migration library.

## Supported drivers

- [pgx/v5](https://github.com/jackc/pgx) single connection and connection pool

In theory, you can also make an implementation for any database using _mig.Database_ interface and instantiate **mig** using _mig.New_ constructor. Since there are other migration libraries supporting multiple databases using Go's standard library's interface _database/sql_, this project has no intention to make such implementations since there is no other library specific to _pgx_ driver. As of now, there is only [tern](https://github.com/jackc/tern) CLI, but it doesn't provide a library.

Custom migration table names must be simple PostgreSQL identifiers such as `schema_migrations` or schema-qualified identifiers such as `app.schema_migrations`. Each identifier part must start with a letter or underscore and contain only letters, digits, and underscores.

`Migrations.Validate` rejects invalid, duplicate, and out-of-order versions. Use
`Migrations.TargetVersion` when a caller needs the newest version from a
validated, non-empty migration set.

## Warning :construction:

This project is in an early stage so you can expect API breaking changes until the first major release.

## Breaking changes in v0.4.0

- `Migrations.Validate` now rejects duplicate and out-of-order versions.
  Previously tolerated manually constructed migration sets with duplicate or
  unsorted versions now fail before database migration begins. Remove duplicate
  versions and sort the remaining set with `sort.Sort(&ms)` before passing it to
  `mig`.
- `ErrInvalidVersion` message text changed from
  `invalid migration version prefix` to `invalid migration version`; match it
  with `errors.Is`, not string comparison.

## Breaking changes in v0.3.0

- Custom database adapters now implement `Migrate(context.Context, mig.Migrations) error` and own their migration orchestration, including locking, migration table setup, migration SQL execution, and version recording.

## Breaking changes in v0.2.0

- Dropped built-in support for [pgx/v4](https://github.com/jackc/pgx/tree/v4).
- Renamed `FromPgxV5` to `FromPgx`.
- Renamed `FromPgxV5Pool` to `FromPgxPool`.

## Naming migration files

```txt
1-initial.sql
2-alter-some-table.sql
...
```

or

```txt
001-initial.sql
002-alter-some-table.sql
```

**mig** will try to parse the number prefix in the correct order without matter if it is prefixed with zeroes or not. So, in theory, the following migration files should also work as expected:

```txt
001-initial.sql
2-alter-some-table.sql
03-insert-some-fixed-data.sql
...
```

## Run tests

- `make start` to start the compose stack with PostgreSQL and [adminer](https://github.com/vrana/adminer)
- `make test` to run all tests
- `make stop` so that new `make start` gets clean database

The Makefile uses `podman-compose` by default. Set `COMPOSE=docker-compose` if you want to run the same targets with Docker Compose.

The local compose services bind to loopback only: PostgreSQL is available at `127.0.0.1:5432`, and Adminer is available at `http://127.0.0.1:8080`.

Set `MIG_TEST_DSN` to point integration tests at a non-default PostgreSQL instance, for example `postgres://postgres@localhost:15432/mig`.

## License

Licensed under either of

- Apache License, Version 2.0
  ([LICENSE-APACHE](LICENSE-APACHE) or http://www.apache.org/licenses/LICENSE-2.0)
- MIT license
  ([LICENSE-MIT](LICENSE-MIT) or http://opensource.org/licenses/MIT)

at your option.

## Contribution

Unless you explicitly state otherwise, any contribution intentionally submitted
for inclusion in the work by you, as defined in the Apache-2.0 license, shall be
dual licensed as above, without any additional terms or conditions.
