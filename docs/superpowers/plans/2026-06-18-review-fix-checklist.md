# Repository Review Fix Checklist

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to address this checklist item-by-item. Mark each checkbox as fixed only after adding or updating tests and running the relevant verification command.

**Goal:** Track the repository review findings from 2026-06-18 so each issue can be fixed, verified, and marked complete.

**Architecture:** Treat each checklist item as an independent review-fix task unless two items naturally share the same code path. Prefer TDD for code changes, keep fixes scoped, and update the README coverage badge whenever Go code or tests change.

**Tech Stack:** Go, pgx/v5, PostgreSQL, GitHub Actions, actionlint, Podman/podman-compose for local container workflows.

---

## Critical

- [x] **Prevent advisory lock leaks when migration context is canceled**
  - Files: `mig.go`, `pgx.go`, `mig_test.go`, `pgx_internal_test.go`
  - Finding: `Mig.Migrate` defers `Unlock(ctx)` with the same caller context used for migration work. Because `pgxDB.Lock` uses a session-level `pg_advisory_lock`, a canceled context can prevent `pg_advisory_unlock` from running and can release a pooled connection that still holds the lock.
  - References: `mig.go:79`, `pgx.go:44`, `pgx.go:122`
  - Fix direction: Use a fresh bounded cleanup context, `context.WithoutCancel`, or a transaction-scoped lock design. Add a regression test that proves unlock is attempted even when the migration context is canceled.
  - Verification: `go test -run 'TestMigrate|TestPgx|Test.*Lock|Test.*Unlock' -count=1 ./...`
  - Fixed: pgx migrations now use `pg_advisory_xact_lock` inside the migration transaction instead of session-level lock/unlock cleanup. Verified with `TestPgxMigrateUsesTransactionScopedAdvisoryLock` against PostgreSQL from `docker-compose.test.yml`.

- [x] **Make migration SQL and version recording atomic**
  - Files: `mig.go`, `pgx.go`, `pgx_internal_test.go`, `mig_test.go`
  - Finding: `pgxDB.RunMigration` commits migration SQL before `Mig.Migrate` records the new version with `SetLastVersion`. If version recording fails after DDL/data commits, the next run can reapply a non-idempotent migration.
  - References: `pgx.go:100`, `pgx.go:115`, `mig.go:96`, `mig.go:100`
  - Fix direction: Record the migration version in the same transaction as the migration SQL. This likely requires changing the `Database` contract or adding a pgx-specific transactional method with tests for rollback on version-update failure.
  - Verification: Add a failing regression test first, then run `go test -run 'TestMigrate|TestRunMigration' -count=1 ./...`
  - Fixed: `Database` now exposes a migration-level operation and pgx records the version in the same transaction as the migration SQL. Verified with `TestPgxMigrateRollsBackMigrationWhenVersionRecordingFails` against PostgreSQL from `docker-compose.test.yml`.

## Important

- [x] **Reject migration version zero**
  - Files: `migrations.go`, `migrations_test.go`
  - Finding: `0.sql` or `000-name.sql` loads as version `0`, but fresh databases also report last version `0`, and `Migrate` only applies migrations with `Version > lastVersion`; the migration is silently skipped forever.
  - References: `migrations.go:61`, `pgx.go:69`, `mig.go:95`
  - Fix direction: Return `ErrInvalidVersion` for parsed version `0`. Add `FromDir` and, if practical, `FromEmbedFS` coverage.
  - Verification: `go test -run 'TestFrom.*Version' -count=1 ./...`
  - Fixed: loader parsing rejects version `0`, and `Mig.Migrate` validates direct `Migrations` construction before database execution. Verified with `TestFromDirReturnsInvalidVersionErrorForZeroVersion` and real PostgreSQL-backed `TestPgxMigrateRejectsZeroVersionMigration`.

- [x] **Return an error for overflowing migration version prefixes**
  - Files: `migrations.go`, `migrations_test.go`
  - Finding: `strconv.ParseUint` errors are ignored, so an overflowing prefix can become `math.MaxUint64` and poison future version ordering/state.
  - References: `migrations.go:61`
  - Fix direction: Check the parse error and wrap `ErrInvalidVersion` with the offending filename.
  - Verification: `go test -run 'TestFrom.*Version' -count=1 ./...`
  - Fixed: loader parsing now checks `strconv.ParseUint` errors and returns `ErrInvalidVersion` for overflowing prefixes. Verified with `TestFromDirReturnsInvalidVersionErrorForOverflowingVersion`.

- [x] **Make custom table names safe SQL identifiers**
  - Files: `mig.go`, `pgx.go`, `pgx_internal_test.go`, `README.md`
  - Finding: `WithCustomTable` accepts arbitrary strings that are interpolated into `CREATE TABLE`, `SELECT`, `UPDATE`, and `INSERT` statements.
  - References: `mig.go:111`, `pgx.go:54`, `pgx.go:64`, `pgx.go:80`, `pgx.go:91`
  - Fix direction: Validate a strict identifier/schema-qualified identifier format or use `pgx.Identifier.Sanitize()`. Document the accepted table-name format.
  - Verification: Add tests for valid identifiers, invalid injection-like names, and schema-qualified names if supported.
  - Fixed: `WithCustomTable` now rejects invalid names with `ErrInvalidTableName`, `FromPgxPool` returns that error before acquiring a connection, `Mig.Migrate` returns it before database execution, and pgx renders accepted names with `pgx.Identifier.Sanitize()`. Documented the accepted identifier format in README. Verified with focused invalid-name tests, schema-qualified real PostgreSQL migration test, and fresh race/coverage test.

- [x] **Enforce or remove the single-row `schema_migrations` assumption**
  - Files: `pgx.go`, `pgx_internal_test.go`
  - Finding: The table schema allows multiple rows because `version bigint PRIMARY KEY` permits many distinct versions. `LastVersion` selects without `ORDER BY` or `LIMIT`, and `SetLastVersion` updates all rows.
  - References: `pgx.go:54`, `pgx.go:64`, `pgx.go:80`
  - Fix direction: Either enforce one row explicitly or switch to append-only version history and query `max(version)`.
  - Verification: Add a database-backed test for multiple existing rows or a deterministic fake test that covers the intended invariant.
  - Fixed: switched pgx to append-only migration history. `lastVersion` now reads `COALESCE(max(version), 0)` and `setLastVersion` inserts the applied version instead of updating all rows. Verified with a real PostgreSQL regression test for pre-existing rows and fresh race/coverage test.

- [ ] **Make the README coverage badge trustworthy**
  - Files: `README.md`, `Makefile`
  - Finding: README advertises `91.2%`, while short/unit-only coverage observed during review was `53.3%`. The full coverage command depends on the expected local PostgreSQL instance and could not be refreshed because `localhost:5432` was occupied by a different database.
  - References: `README.md:6`, `Makefile:13`, `AGENTS.md:9`
  - Fix direction: After code/test fixes, run the real coverage command against the intended PostgreSQL service and update the badge to match the generated total.
  - Verification: `make test`

- [ ] **Use non-mutating lint in CI**
  - Files: `Makefile`, `.github/workflows/pipeline.yaml`
  - Finding: CI delegates to `make lint`, and `make lint` runs `golangci-lint run --fix`. CI should validate committed code, not rewrite the checkout.
  - References: `Makefile:3`, `.github/workflows/pipeline.yaml:20`
  - Fix direction: Add a non-mutating lint target for CI, keep a separate local fix target if desired, and point the workflow at the non-mutating target.
  - Verification: `make lint` or the new CI lint target, plus `actionlint .github/workflows/pipeline.yaml .github/workflows/update-deps.yaml`

- [ ] **Bind local compose services to loopback**
  - Files: `docker-compose.yml`, `README.md`
  - Finding: Local compose exposes trust-auth PostgreSQL on `5432:5432` and Adminer on `8080:8080`, which can expose passwordless database access beyond the local machine.
  - References: `docker-compose.yml:8`, `docker-compose.yml:10`, `docker-compose.yml:19`
  - Fix direction: Bind both published ports to `127.0.0.1`.
  - Verification: `podman-compose config`

- [ ] **Add Go module Dependabot coverage or document the alternative**
  - Files: `.github/dependabot.yml`, `.github/workflows/update-deps.yaml`
  - Finding: Dependabot only watches GitHub Actions. Go module updates rely on a scheduled reusable workflow with a PAT secret.
  - References: `.github/dependabot.yml:8`, `.github/workflows/update-deps.yaml:5`, `go.mod:5`
  - Fix direction: Add a `gomod` Dependabot entry unless the reusable workflow is intentionally the dependency-update policy, in which case document that decision.
  - Verification: `actionlint .github/workflows/pipeline.yaml .github/workflows/update-deps.yaml`

## Minor

- [ ] **Harden migration loader tests with length and SQL-content assertions**
  - Files: `migrations_test.go`
  - Finding: `TestFromDir` and `TestFromEmbedFS` iterate over `got` without asserting `len(got) == len(want)`, so an empty result could pass.
  - References: `migrations_test.go:34`, `migrations_test.go:53`
  - Fix direction: Assert lengths before element checks and include SQL content checks.
  - Verification: `go test -run 'TestFromDir|TestFromEmbedFS' -count=1 ./...`

- [ ] **Make short tests hermetic or clearly separate examples/integration tests**
  - Files: `mig_test.go`, `README.md`, `Makefile`
  - Finding: `go test -short ./...` still runs examples that connect to local PostgreSQL, so the short suite fails when the expected local database is unavailable.
  - References: `mig_test.go:217`, `mig_test.go:252`
  - Fix direction: Move database-backed examples behind a testable integration setup, remove `// Output:` from examples that require PostgreSQL, or document/run a separate unit-only command.
  - Verification: `go test -short ./...`

- [ ] **Document Podman-based local workflow**
  - Files: `README.md`, `Makefile`
  - Finding: README and Makefile use `docker-compose`, but local policy says Podman is available and should be used unless Docker is specifically required.
  - References: `README.md:52`, `Makefile:7`
  - Fix direction: Switch commands to `podman-compose` or make the compose command configurable.
  - Verification: `make start` and `make stop` with Podman, or a documented equivalent.

- [ ] **Pin the Adminer image used by local compose**
  - Files: `docker-compose.yml`
  - Finding: `adminer` is unpinned, so local tooling can change unexpectedly as the tag moves.
  - References: `docker-compose.yml:12`
  - Fix direction: Pin to a concrete supported Adminer tag.
  - Verification: `podman-compose config`

## Repository Reminders

- [ ] **Confirm GitHub CodeQL/code scanning is enabled**
  - This repository contains Go code. If CodeQL/code scanning is not enabled in GitHub repository settings or workflows, enable it or document why it is intentionally omitted.

- [x] **Keep blocking GitHub Actions workflow linting**
  - The main pipeline already includes a blocking `actionlint` job using `raven-actions/actionlint@v2`.
  - References: `.github/workflows/pipeline.yaml:9`

- [x] **Container image vulnerability gate not applicable to current tree**
  - No tracked `Dockerfile` was present during review, so the image vulnerability scanning reminder does not currently apply.

## Review Verification Notes

- `actionlint .github/workflows/pipeline.yaml .github/workflows/update-deps.yaml` passed during review.
- `go mod verify` passed during review.
- `go test -run '^Test' -short -count=1 -race -coverprofile=coverage.out ./...` passed during review with `53.3%` coverage.
- Full non-cached `go test -count=1 -race -coverprofile=coverage.out ./...` failed because local `localhost:5432` was not the expected trust-auth PostgreSQL test database.
- `go test -short ./...` failed because database-backed examples still attempted to connect to PostgreSQL.
