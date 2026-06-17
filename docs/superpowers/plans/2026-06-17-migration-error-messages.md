# Migration Error Messages Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Improve migration failure diagnostics while preserving the existing public API and wrapped error behavior, including caller access to pgx `*pgconn.PgError` through `errors.As`.

**Architecture:** Keep errors as ordinary wrapped Go errors. Add deterministic unit coverage around `Mig.Migrate` using a fake `Database`, and around `pgxDB.RunMigration` using small internal fakes for `pgxConn` and `pgx.Tx`. Make the smallest production changes needed: named return error in `Migrate`, clearer pgx transaction labels, and `errors.Join` when cleanup fails alongside a primary error. Do not include migration SQL text in default error messages; callers that need structured PostgreSQL diagnostics should use `errors.As` with `*pgconn.PgError`.

**Tech Stack:** Go standard library, existing pgx v5 dependency, existing `go test` and `make test` targets.

---

## File Structure

- Modify `mig_test.go`: add public-package tests for `Mig.Migrate` error behavior using `dbFake`.
- Modify `mig.go`: make `Migrate` use a named return error and wrap unlock failures as `unlock: %w`.
- Modify `pgx_internal_test.go`: add internal fake transaction tests for `pgxDB.RunMigration` error wrapping.
- Modify `pgx.go`: replace generic migration transaction wrappers with operation-specific messages and preserve both exec and rollback errors.
- Optionally modify `README.md`: document how callers can extract `*pgconn.PgError` from returned errors with `errors.As`, if public documentation is desired.

## Task 1: `Mig.Migrate` Unlock And Migration Context

**Files:**
- Modify: `mig_test.go`
- Modify: `mig.go`

- [ ] **Step 1: Write failing `Mig.Migrate` tests**

Add `errors` and `strings` imports to `mig_test.go`.

Extend `dbFake` with deterministic error fields:

```go
type dbFake struct {
	l bool
	v uint64

	lockErr         error
	createTableErr  error
	lastVersionErr  error
	runMigrationErr error
	setVersionErr   error
	unlockErr       error
}
```

Update its methods to return configured errors:

```go
func (db *dbFake) Lock(context.Context) error {
	db.l = true

	return db.lockErr
}

func (db *dbFake) CreateSchemaMigrationsTable(context.Context) error {
	return db.createTableErr
}

func (db *dbFake) LastVersion(context.Context) (uint64, error) {
	return db.v, db.lastVersionErr
}

func (db *dbFake) SetLastVersion(_ context.Context, lastVersion uint64) error {
	if db.setVersionErr != nil {
		return db.setVersionErr
	}

	db.v = lastVersion

	return nil
}

func (db *dbFake) RunMigration(context.Context, string) error {
	return db.runMigrationErr
}

func (db *dbFake) Unlock(context.Context) error {
	db.l = false

	return db.unlockErr
}
```

Add tests:

```go
func TestMigrateReturnsUnlockError(t *testing.T) {
	t.Parallel()

	unlockErr := errors.New("unlock failed")
	db := &dbFake{unlockErr: unlockErr} //nolint:exhaustruct
	m := mig.New(mig.Migrations{}, db)

	err := m.Migrate(context.Background())
	if !errors.Is(err, unlockErr) {
		t.Fatalf("Migrate() error=%v; want unlock error", err)
	}

	if !strings.Contains(err.Error(), "unlock: unlock failed") {
		t.Fatalf("Migrate() error=%q; want unlock context", err)
	}
}

func TestMigrateJoinsMigrationAndUnlockErrors(t *testing.T) {
	t.Parallel()

	runErr := errors.New("migration failed")
	unlockErr := errors.New("unlock failed")
	db := &dbFake{
		runMigrationErr: runErr,
		unlockErr:       unlockErr,
	} //nolint:exhaustruct
	m := mig.New(mig.Migrations{{
		Version: 7,
		Path:    "007-broken.sql",
		SQL:     "broken",
	}}, db)

	err := m.Migrate(context.Background())
	if !errors.Is(err, runErr) {
		t.Fatalf("Migrate() error=%v; want migration error", err)
	}

	if !errors.Is(err, unlockErr) {
		t.Fatalf("Migrate() error=%v; want unlock error", err)
	}

	for _, want := range []string{
		"run migration 7 from file 007-broken.sql: migration failed",
		"unlock: unlock failed",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("Migrate() error=%q; want %q", err, want)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify RED**

Run:

```bash
go test ./... -run 'TestMigrate(ReturnsUnlockError|JoinsMigrationAndUnlockErrors)'
```

Expected: fail because unlock errors are currently dropped.

- [ ] **Step 3: Implement minimal `Migrate` change**

Change the signature and defer in `mig.go`:

```go
func (d *Mig) Migrate(ctx context.Context) (err error) {
	err = d.db.Lock(ctx)
	if err != nil {
		return fmt.Errorf("lock: %w", err)
	}

	defer func() {
		if unlockErr := d.db.Unlock(ctx); unlockErr != nil {
			err = errors.Join(err, fmt.Errorf("unlock: %w", unlockErr))
		}
	}()
```

Leave the existing return statements unchanged.

- [ ] **Step 4: Run tests to verify GREEN**

Run:

```bash
go test ./... -run 'TestMigrate(ReturnsUnlockError|JoinsMigrationAndUnlockErrors)'
```

Expected: pass.

## Task 2: `pgxDB.RunMigration` Transaction Error Messages

**Files:**
- Modify: `pgx_internal_test.go`
- Modify: `pgx.go`

- [ ] **Step 1: Write failing pgx transaction tests**

Add `errors` and `strings` imports to `pgx_internal_test.go`.

Add an internal fake connection and transaction:

```go
type txFake struct {
	execErr     error
	rollbackErr error
	commitErr   error
}

func (tx *txFake) Begin(context.Context) (pgx.Tx, error) {
	return nil, errors.New("unexpected nested transaction")
}

func (tx *txFake) Commit(context.Context) error {
	return tx.commitErr
}

func (tx *txFake) Rollback(context.Context) error {
	return tx.rollbackErr
}

func (tx *txFake) CopyFrom(context.Context, pgx.Identifier, []string, pgx.CopyFromSource) (int64, error) {
	return 0, errors.New("unexpected copy from")
}

func (tx *txFake) SendBatch(context.Context, *pgx.Batch) pgx.BatchResults {
	return nil
}

func (tx *txFake) LargeObjects() pgx.LargeObjects {
	return pgx.LargeObjects{}
}

func (tx *txFake) Prepare(context.Context, string, string) (*pgconn.StatementDescription, error) {
	return nil, errors.New("unexpected prepare")
}

func (tx *txFake) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, tx.execErr
}

func (tx *txFake) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, errors.New("unexpected query")
}

func (tx *txFake) QueryRow(context.Context, string, ...any) pgx.Row {
	return rowFake{}
}

func (tx *txFake) Conn() *pgx.Conn {
	return nil
}

type connFake struct {
	tx       *txFake
	beginErr error
}

func (conn connFake) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, errors.New("unexpected exec")
}

func (conn connFake) QueryRow(context.Context, string, ...any) pgx.Row {
	return rowFake{}
}

func (conn connFake) Begin(context.Context) (pgx.Tx, error) {
	if conn.beginErr != nil {
		return nil, conn.beginErr
	}

	return conn.tx, nil
}

type rowFake struct{}

func (rowFake) Scan(...any) error {
	return errors.New("unexpected scan")
}
```

Add tests:

```go
func TestRunMigrationWrapsExecError(t *testing.T) {
	t.Parallel()

	execErr := errors.New("syntax failed")
	db := newPgxDB(connFake{tx: &txFake{execErr: execErr}}, "")

	err := db.RunMigration(context.Background(), "broken")
	if !errors.Is(err, execErr) {
		t.Fatalf("RunMigration() error=%v; want exec error", err)
	}

	if !strings.Contains(err.Error(), "execute migration SQL: syntax failed") {
		t.Fatalf("RunMigration() error=%q; want execute migration SQL context", err)
	}
}

func TestRunMigrationJoinsExecAndRollbackErrors(t *testing.T) {
	t.Parallel()

	execErr := errors.New("syntax failed")
	rollbackErr := errors.New("rollback failed")
	db := newPgxDB(connFake{tx: &txFake{
		execErr:     execErr,
		rollbackErr: rollbackErr,
	}}, "")

	err := db.RunMigration(context.Background(), "broken")
	if !errors.Is(err, execErr) {
		t.Fatalf("RunMigration() error=%v; want exec error", err)
	}

	if !errors.Is(err, rollbackErr) {
		t.Fatalf("RunMigration() error=%v; want rollback error", err)
	}

	for _, want := range []string{
		"execute migration SQL: syntax failed",
		"rollback migration transaction: rollback failed",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("RunMigration() error=%q; want %q", err, want)
		}
	}
}
```

- [ ] **Step 1b: Add PostgreSQL error extraction test or documentation**

If adding a code-level regression test, add a test that wraps a `*pgconn.PgError` through `RunMigration` and proves it is still extractable:

```go
func TestRunMigrationPreservesPgError(t *testing.T) {
	t.Parallel()

	pgErr := &pgconn.PgError{
		Message:  "syntax error at or near \"TABLE\"",
		Severity: "ERROR",
		Code:     "42601",
	}
	db := newPgxDB(connFake{tx: &txFake{execErr: pgErr}}, "")

	err := db.RunMigration(context.Background(), "broken")

	var got *pgconn.PgError
	if !errors.As(err, &got) {
		t.Fatalf("RunMigration() error=%v; want pg error", err)
	}

	if got.SQLState() != "42601" {
		t.Fatalf("PgError.SQLState()=%q; want %q", got.SQLState(), "42601")
	}
}
```

If documenting instead, add a short README example:

```go
var pgErr *pgconn.PgError
if errors.As(err, &pgErr) {
	fmt.Println(pgErr.Message)
	fmt.Println(pgErr.Detail)
	fmt.Println(pgErr.Hint)
	fmt.Println(pgErr.Position)
	fmt.Println(pgErr.SQLState())
}
```

Do not add migration SQL text to default error strings.

- [ ] **Step 2: Run tests to verify RED**

Run:

```bash
go test ./... -run 'TestRunMigration(WrapsExecError|JoinsExecAndRollbackErrors|PreservesPgError)'
```

Expected: fail because the current message is `exec`, and rollback failure hides the exec failure.

- [ ] **Step 3: Implement minimal `RunMigration` change**

Change `pgx.go`:

```go
tx, err := db.conn.Begin(ctx)
if err != nil {
	return fmt.Errorf("begin migration transaction: %w", err)
}

if _, err := tx.Exec(ctx, query); err != nil {
	err = fmt.Errorf("execute migration SQL: %w", err)
	if rollbackErr := tx.Rollback(ctx); rollbackErr != nil {
		return errors.Join(err, fmt.Errorf("rollback migration transaction: %w", rollbackErr))
	}

	return err
}

if err := tx.Commit(ctx); err != nil {
	return fmt.Errorf("commit migration transaction: %w", err)
}
```

- [ ] **Step 4: Run tests to verify GREEN**

Run:

```bash
go test ./... -run 'TestRunMigration(WrapsExecError|JoinsExecAndRollbackErrors|PreservesPgError)'
```

Expected: pass.

## Task 3: Full Verification

**Files:**
- Verify all modified files.

- [ ] **Step 1: Run unit and integration tests**

Run:

```bash
go test ./...
```

Expected: pass. If PostgreSQL is not running, start it with `podman-compose up -d postgres`.

- [ ] **Step 2: Run coverage gate**

Run:

```bash
make test
```

Expected: pass with coverage at or above 90%.

- [ ] **Step 3: Stop local PostgreSQL if started for this work**

Run:

```bash
podman-compose down
```

Expected: Postgres service is stopped.

- [ ] **Step 4: Check whitespace and final status**

Run:

```bash
git diff --check
git status --short --branch
```

Expected: no whitespace errors. Branch should remain ahead of origin by the spec commit plus uncommitted implementation changes for human review.
