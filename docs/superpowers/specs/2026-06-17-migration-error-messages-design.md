# Migration Error Messages Design

## Summary

`mig` currently wraps migration failures with useful high-level context, but some lower-level messages are too generic and one cleanup path does not affect the returned error. This change should improve migration failure diagnostics without adding new public error types, sentinels, options, or dependencies.

The package should continue returning ordinary wrapped Go errors. Callers should still be able to use `errors.Is` and `errors.As` with underlying errors, including pgx errors.

## Goals

- Return unlock failures from `Mig.Migrate`.
- Preserve both the primary migration failure and the unlock failure when both happen.
- Preserve both the migration SQL execution failure and rollback failure when both happen.
- Keep migration failure messages anchored to the migration version and file path.
- Replace generic pgx migration execution labels such as `exec` with clearer operation-specific context.
- Add tests that lock in the improved failure behavior and representative messages.

## Non-Goals

- Do not introduce exported typed errors.
- Do not introduce exported sentinel errors.
- Do not change the public `Database` interface.
- Do not change migration ordering, storage schema, advisory lock behavior, or pgx connection handling.
- Do not include SQL text in returned errors.
- Do not add third-party dependencies.

## Current Behavior

`Mig.Migrate` currently wraps failed migration SQL with version and path context:

```txt
run migration 2 from file 02.sql: exec: ERROR: ...
```

The outer context is useful and should stay. The inner `exec` label is generic and does not explain which operation failed.

`Mig.Migrate` also attempts to join unlock failures with the main error:

```go
defer func() {
    err = errors.Join(err, d.db.Unlock(ctx))
}()
```

However, `Migrate` does not use a named return value, so this deferred assignment does not change the returned error. Unlock failures are effectively dropped.

`pgxDB.RunMigration` starts a transaction, executes the migration SQL, rolls back on execution failure, and commits on success. If SQL execution fails and rollback also fails, the current code returns only the rollback failure. That hides the original SQL execution failure, which is usually the more useful error for users.

## Desired Behavior

`Mig.Migrate` should use a named return error. The deferred unlock should assign back to that named return value using `errors.Join`.

When migration work succeeds but unlocking fails, `Migrate` should return an unlock error:

```txt
unlock: ...
```

When migration work fails and unlocking also fails, `Migrate` should return an error that contains both:

```txt
run migration 2 from file 02.sql: execute migration SQL: ...
unlock: ...
```

The exact formatting may follow Go's `errors.Join` formatting, but both messages must be discoverable through `Error()` and the underlying errors must remain unwrap-able.

When migration SQL execution fails and rollback succeeds, the returned error should look like:

```txt
execute migration SQL: ...
```

When migration SQL execution fails and rollback also fails, the returned error should include both:

```txt
execute migration SQL: ...
rollback migration transaction: ...
```

Rollback failure must not hide the original SQL execution failure.

## Error Message Vocabulary

The pgx migration execution path should use operation-specific labels:

- `begin migration transaction`
- `execute migration SQL`
- `rollback migration transaction`
- `commit migration transaction`

The high-level migrator should keep existing context where it is already helpful:

- `lock`
- `create schema migrations table`
- `last version`
- `run migration <version> from file <path>`
- `set last version <version>`
- `unlock`

This pass should focus on migration failure diagnostics. It may leave non-migration pgx helper messages such as schema table creation and advisory lock internals unchanged unless a test requires touching them.

## Testing

Tests should be written before implementation.

Unit tests around `Mig.Migrate` should use a fake `Database` implementation so they can force each failure deterministically:

- successful migration with unlock failure returns an unlock error
- migration failure with unlock failure returns both errors
- migration failure includes migration version and path context
- wrapped underlying errors remain discoverable with `errors.Is`

Tests around `pgxDB.RunMigration` should verify:

- a broken migration SQL error includes `execute migration SQL`
- rollback failure does not hide the original execution failure
- the rollback failure is also included

The rollback failure case should avoid fragile database-state tricks if possible. A small fake implementation of the private `pgxConn` and `pgx.Tx` contracts is acceptable in internal tests because it keeps the failure deterministic and does not add dependencies.

Integration tests may still exercise real PostgreSQL behavior for representative pgx error messages, but the edge cases should be covered by deterministic unit tests.

## Acceptance Criteria

- `Mig.Migrate` returns unlock errors.
- `Mig.Migrate` joins unlock errors with an existing migration error when both occur.
- `pgxDB.RunMigration` returns both SQL execution and rollback errors when both occur.
- Migration failure messages include version and file path.
- pgx migration execution errors use operation-specific labels instead of plain `exec`.
- Existing public API remains unchanged.
- Existing underlying errors remain available through normal Go error wrapping.
- `go test ./...` passes.
- `make test` passes when PostgreSQL is running.
- `git diff --check` passes.
