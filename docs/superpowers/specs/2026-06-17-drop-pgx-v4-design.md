# Drop pgx v4 Support Design

## Summary

`mig` is currently released as `v0.1.11`, so this breaking change should stay in the pre-`v1` line instead of becoming a Go module `v2`. The next release should be `v0.2.0`, keep the module path as `go.acim.net/mig`, and clearly document that built-in `github.com/jackc/pgx/v4` support was removed.

The implementation should make `github.com/jackc/pgx/v5` the only built-in pgx integration. Since this is a breaking pre-`v1` release, the public constructors should also be renamed from `FromPgxV5` and `FromPgxV5Pool` to `FromPgx` and `FromPgxPool`.

## Goals

- Remove all direct `github.com/jackc/pgx/v4` dependencies from production code, examples, and tests.
- Keep the module path as `go.acim.net/mig`.
- Prepare the change for a `v0.2.0` release, not `v2.0.0`.
- Make pgx v5 the default naming surface with `FromPgx` and `FromPgxPool`.
- Document the breaking change clearly for users migrating from `v0.1.x`.

## Non-Goals

- Do not create a `/v2` Go module path.
- Do not tag or publish a release as part of implementation.
- Do not add support for other database drivers.
- Do not redesign the migration storage schema or migration execution behavior.
- Do not remove the generic `Database` interface or `New` constructor.

## Public API

The package should expose these pgx helpers:

```go
func FromPgx(ms Migrations, conn *pgx.Conn, opts ...Option) *Mig
func FromPgxPool(ms Migrations, pool *pgxpool.Pool, opts ...Option) (*Mig, func(), error)
```

The package should no longer expose these helpers:

```go
func FromPgxV4(...)
func FromPgxV4Pool(...)
func FromPgxV5(...)
func FromPgxV5Pool(...)
```

Removing `FromPgxV5` and `FromPgxV5Pool` is intentional because the project is still pre-`v1` and this is a good moment to make the pgx v5-only API clean.

## Module And Release Versioning

`go.mod` should remain:

```go
module go.acim.net/mig
```

The release should be tagged later as:

```txt
v0.2.0
```

This follows Go module conventions: `/v2` module paths are needed for `v2.0.0` and later, but not for breaking changes before `v1.0.0`.

## Implementation Shape

Production code should remove all pgx v4 imports and adapters:

- Remove `github.com/jackc/pgx/v4`.
- Remove `github.com/jackc/pgx/v4/pgxpool`.
- Remove `pgx4conn`, `pgx4pool`, and `tx4`.
- Update `LastVersion` error handling to check only the pgx v5 no-rows error.
- Rename the pgx v5 adapter-facing constructors to `FromPgx` and `FromPgxPool`.
- Keep the internal abstraction around `pgxCmds`, `scan`, `tx`, and `rowsAffected` if it remains useful for sharing single-connection and pool-connection behavior.

Tests and examples should match the new API:

- Delete pgx v4 examples and integration tests.
- Rename pgx v5 examples to `ExampleFromPgx` and `ExampleFromPgxPool`.
- Update internal interface assertions so only pgx v5 adapter types are asserted.
- Keep existing migration behavior tests intact.

Docs should match the new supported surface:

- README supported drivers should list only pgx v5 single connection and connection pool.
- README warning should mention the current pre-`v1` status if it remains accurate.
- Release notes for `v0.2.0` should call out the breaking change:
  `Breaking: dropped support for github.com/jackc/pgx/v4; pgx v5 is now the only built-in pgx driver support. Rename FromPgxV5 to FromPgx and FromPgxV5Pool to FromPgxPool.`

## Error Handling

No runtime behavior change is expected except the removal of pgx v4 compatibility. Errors should continue to be wrapped with the current contextual messages such as `acquire connection`, `begin`, `exec`, `scan`, and `commit`.

`LastVersion` should continue treating an empty migration table as version `0`, using the pgx v5 no-rows sentinel.

## Testing

Verification should include:

- `go test ./...`
- Existing long/integration tests when PostgreSQL is running through the current local compose setup.
- A dependency check such as `go mod tidy` followed by verifying that pgx v4 no longer appears in `go.mod`.

If the local database is not running, the implementation should at least run the short test set and report that database-backed tests were not executed.

## Repository Hygiene Notes

This repository has GitHub Actions workflows under `.github/workflows/`, so a follow-up should add a blocking `actionlint` CI gate. The preferred job is:

```yaml
actionlint:
  runs-on: ubuntu-latest
  steps:
    - uses: actions/checkout@v6

    - uses: raven-actions/actionlint@v2
```

This repository contains Go code, so enabling GitHub CodeQL/code scanning is also recommended as a follow-up. There is no Dockerfile in the repository, so the container image vulnerability scanning reminder does not apply to the current tree.

## Acceptance Criteria

- `go.mod` still says `module go.acim.net/mig`.
- `go.mod` no longer requires `github.com/jackc/pgx/v4`.
- No production, test, or README references remain for pgx v4 support.
- The public pgx constructors are `FromPgx` and `FromPgxPool`.
- Tests and examples compile against the new names.
- README describes pgx v5 as the only built-in driver support.
- The implementation is suitable for a future `v0.2.0` release.
