# mig

[![pipeline](https://github.com/acim/mig/actions/workflows/pipeline.yaml/badge.svg)](https://github.com/acim/mig/actions/workflows/pipeline.yaml)
[![Go Reference](https://pkg.go.dev/badge/go.acim.net/mig.svg)](https://pkg.go.dev/go.acim.net/mig)
[![Go Report](https://goreportcard.com/badge/go.acim.net/mig)](https://goreportcard.com/report/go.acim.net/mig)
![Go Coverage](https://img.shields.io/badge/coverage-80.8%25-brightgreen?style=flat&logo=go)

Go PostgreSQL database schema migration library.

## Supported drivers

- [pgx/v5](https://github.com/jackc/pgx) single connection and connection pool
- [pgx/v4](https://github.com/jackc/pgx/tree/v4) single connection and connection pool

In theory, you can also make an implementation for any database using _mig.Database_ interface and instantiate **mig** using _mig.New_ constructor. Since there are other migration libraries supporting multiple databases using Go's standard library's interface _database/sql_, this project has no intention to make such implementations since there is no other library specific to _pgx_ driver. As of now, there is only [tern](https://github.com/jackc/tern) CLI, but it doesn't provide a library.

## Warning :construction:

This project is in an early stage so you can expect API breaking changes until the first major release.

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

- `make start` to start the docker-compose with PostgreSQL and [adminer](https://github.com/vrana/adminer)
- `make test` to run all tests
- `make stop` so that new `make start` gets clean database

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
