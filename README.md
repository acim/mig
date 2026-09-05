# mig

[![pipeline](https://github.com/acim/mig/actions/workflows/pipeline.yaml/badge.svg)](https://github.com/acim/mig/actions/workflows/pipeline.yaml)
[![Go Reference](https://pkg.go.dev/badge/go.acim.net/mig.svg)](https://pkg.go.dev/go.acim.net/mig)
[![Go Report](https://goreportcard.com/badge/go.acim.net/mig)](https://goreportcard.com/report/go.acim.net/mig)
![Go Coverage](https://img.shields.io/badge/coverage-97.2%25-brightgreen?style=flat&logo=go)

Go PostgreSQL database schema migration library.

## Supported drivers

- [pgx/v5](https://github.com/jackc/pgx) single connection and connection pool

In theory, you can also make an implementation for any database using _mig.Database_ interface and instantiate **mig** using _mig.New_ constructor. Since there are other migration libraries supporting multiple databases using Go's standard library's interface _database/sql_, this project has no intention to make such implementations since there is no other library specific to _pgx_ driver. As of now, there is only [tern](https://github.com/jackc/tern) CLI, but it doesn't provide a library.

Custom migration table names must be simple PostgreSQL identifiers such as `schema_migrations` or schema-qualified identifiers such as `app.schema_migrations`. Each identifier part must start with a letter or underscore, contain only letters, digits, and underscores, and be at most 63 bytes long.

The pgx adapter owns the migration transaction. Migration SQL must not contain
top-level transaction-control statements such as `BEGIN`, `COMMIT`, `ROLLBACK`,
`SAVEPOINT`, or their PostgreSQL equivalents. Such migrations return
`ErrTransactionControl` without recording the version. The adapter also verifies
that its transaction remains active before recording each version and before
committing. `SAVEPOINT` and `RELEASE` are deliberately prohibited even though
they cannot end the transaction, so migration SQL never manages transaction
state owned by the library.

`WithAcquireConnectionTimeout` applies only to `FromPgxPool`, where it limits
connection acquisition. `New` and `FromPgx` reject that pool-specific option
with `ErrUnsupportedOption`.

`Migrations.Validate` rejects invalid, duplicate, and out-of-order versions. Use
`Migrations.TargetVersion` when a caller needs the newest version from a
validated, non-empty migration set.

## Warning :construction:

This project is in an early stage so you can expect API breaking changes until the first major release.

## Breaking changes in v0.4.0

- Migration SQL can no longer contain top-level transaction-control statements.
  This includes `SAVEPOINT` and `RELEASE`, which are prohibited deliberately so
  the library exclusively owns transaction state. Match the resulting error
  with `errors.Is(err, ErrTransactionControl)`.
- `WithAcquireConnectionTimeout` is supported only by `FromPgxPool`. `New` and
  `FromPgx` now return `ErrUnsupportedOption` from `Migrate` when it is supplied.
- Custom table-name parts longer than PostgreSQL's 63-byte identifier limit now
  fail with `ErrInvalidTableName` instead of being silently truncated.
- The pgx advisory-lock key changed to use the canonical ledger identity and a
  full-width hash. During an upgrade, do not allow v0.3.x and v0.4.0 instances
  to run migrations concurrently: scale migrating replicas to one, or otherwise
  ensure all v0.3.x migrators have stopped before starting a v0.4.0 migrator.
- `New`, `FromPgxPool`, and `FromPgx` now report `ErrNilDatabase` for nil database
  dependencies instead of allowing a later panic.
- New exported errors are `ErrTransactionControl`, `ErrTransactionInactive`,
  `ErrNilDatabase`, and `ErrUnsupportedOption`; use `errors.Is` rather than
  comparing their message text.
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

The Makefile uses `podman compose` by default. Run `make start COMPOSE="docker compose"` if you want to use Docker Compose instead.

The local compose services bind to loopback only: PostgreSQL is available at `127.0.0.1:5432`, and Adminer is available at `http://127.0.0.1:8080`.

Set `MIG_TEST_DSN` to point integration tests at a non-default PostgreSQL instance, for example `postgres://postgres@localhost:15432/mig`.
The repository's alternate test stack publishes that address; start it with
`podman compose -f docker-compose.test.yml up -d` and stop it with
`podman compose -f docker-compose.test.yml down`.

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
