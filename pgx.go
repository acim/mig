package mig

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrTransactionControl  = errors.New("migration SQL contains transaction control statement")
	ErrTransactionInactive = errors.New("migration transaction is not active")
)

type pgxConn interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Begin(ctx context.Context) (pgx.Tx, error)
}

type pgxExecutor interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type pgxDB struct {
	table         string
	tableLockName string
	lockID        int64
	conn          pgxConn
}

func newPgxDB(conn pgxConn, tableName string) *pgxDB {
	db := &pgxDB{
		table:         sanitizeTableName(tableName),
		tableLockName: tableName,
		conn:          conn,
	}

	return db
}

func sanitizeTableName(tableName string) string {
	return pgx.Identifier(strings.Split(tableName, ".")).Sanitize()
}

func (db *pgxDB) createSchemaMigrationsTable(ctx context.Context, exec pgxExecutor) error {
	q := fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (version bigint PRIMARY KEY)", db.table)

	if _, err := exec.Exec(ctx, q); err != nil {
		return fmt.Errorf("exec: %w", err)
	}

	return nil
}

func (db *pgxDB) lastVersion(ctx context.Context, exec pgxExecutor) (uint64, error) {
	q := "SELECT COALESCE(max(version), 0) FROM " + db.table

	var version uint64

	if err := exec.QueryRow(ctx, q).Scan(&version); err != nil {
		return 0, fmt.Errorf("scan: %w", err)
	}

	return version, nil
}

func (db *pgxDB) setLastVersion(ctx context.Context, exec pgxExecutor, lastVersion uint64) error {
	q := fmt.Sprintf("INSERT INTO %s (version) VALUES ($1)", db.table)

	if _, err := exec.Exec(ctx, q, lastVersion); err != nil {
		return fmt.Errorf("exec: %w", err)
	}

	return nil
}

func (db *pgxDB) Migrate(ctx context.Context, ms Migrations) (err error) {
	if err := db.setLockID(ctx); err != nil {
		return fmt.Errorf("set lock id: %w", err)
	}

	tx, err := db.conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin migration transaction: %w", err)
	}

	done := false
	defer func() {
		if done {
			return
		}

		if rollbackErr := tx.Rollback(ctx); rollbackErr != nil {
			err = errors.Join(err, fmt.Errorf("rollback migration transaction: %w", rollbackErr))
		}
	}()

	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", db.lockID); err != nil {
		return fmt.Errorf("lock migration transaction: %w", err)
	}

	if err := db.createSchemaMigrationsTable(ctx, tx); err != nil {
		return fmt.Errorf("create schema migrations table: %w", err)
	}

	lastVersion, err := db.lastVersion(ctx, tx)
	if err != nil {
		return fmt.Errorf("last version: %w", err)
	}

	for _, m := range ms {
		if m.Version <= lastVersion {
			continue
		}

		if containsTransactionControl(m.SQL) {
			return fmt.Errorf("run migration %d from file %s: %w", m.Version, m.Path, ErrTransactionControl)
		}

		if _, err := tx.Exec(ctx, m.SQL); err != nil {
			return fmt.Errorf("run migration %d from file %s: execute migration SQL: %w", m.Version, m.Path, err)
		}

		if tx.Conn() == nil || tx.Conn().PgConn().TxStatus() != 'T' {
			return fmt.Errorf("run migration %d from file %s: %w", m.Version, m.Path, ErrTransactionInactive)
		}

		if err := db.setLastVersion(ctx, tx, m.Version); err != nil {
			return fmt.Errorf("set last version %d: %w", m.Version, err)
		}
	}

	if tx.Conn() == nil || tx.Conn().PgConn().TxStatus() != 'T' {
		return ErrTransactionInactive
	}

	if err := tx.Commit(ctx); err != nil {
		done = true
		return fmt.Errorf("commit migration transaction: %w", err)
	}

	done = true

	return nil
}

func containsTransactionControl(sql string) bool {
	statementWords := make([]string, 0, 5)

	for i := 0; i < len(sql); {
		switch {
		case sql[i] == ';':
			if transactionControlWords(statementWords) {
				return true
			}
			statementWords = statementWords[:0]
			i++
		case sql[i] == '-' && i+1 < len(sql) && sql[i+1] == '-':
			i += 2
			for i < len(sql) && sql[i] != '\n' {
				i++
			}
		case sql[i] == '/' && i+1 < len(sql) && sql[i+1] == '*':
			i = skipBlockComment(sql, i+2)
		case sql[i] == '\'' || sql[i] == '"':
			i = skipQuotedSQL(sql, i, sql[i], hasEscapeStringPrefix(sql, i))
		case sql[i] == '$':
			if end := dollarQuoteEnd(sql, i); end > i {
				i = end
			} else {
				i++
			}
		case isSQLWordByte(sql[i]):
			start := i
			for i < len(sql) && isSQLWordByte(sql[i]) {
				i++
			}
			if len(statementWords) < cap(statementWords) {
				statementWords = append(statementWords, strings.ToUpper(sql[start:i]))
			}
		default:
			i++
		}
	}

	return transactionControlWords(statementWords)
}

func transactionControlWords(words []string) bool {
	if len(words) == 0 {
		return false
	}

	switch words[0] {
	case "ABORT", "BEGIN", "COMMIT", "END", "RELEASE", "ROLLBACK", "SAVEPOINT":
		return true
	case "PREPARE", "START":
		return len(words) > 1 && words[1] == "TRANSACTION"
	case "SET":
		return len(words) > 1 && (words[1] == "TRANSACTION" ||
			(len(words) > 4 && words[1] == "SESSION" && words[2] == "CHARACTERISTICS" &&
				words[3] == "AS" && words[4] == "TRANSACTION"))
	default:
		return false
	}
}

func skipBlockComment(sql string, i int) int {
	depth := 1
	for i < len(sql) && depth > 0 {
		switch {
		case i+1 < len(sql) && sql[i] == '/' && sql[i+1] == '*':
			depth++
			i += 2
		case i+1 < len(sql) && sql[i] == '*' && sql[i+1] == '/':
			depth--
			i += 2
		default:
			i++
		}
	}

	return i
}

func skipQuotedSQL(sql string, i int, quote byte, backslashEscapes bool) int {
	for i++; i < len(sql); i++ {
		if sql[i] == '\\' && backslashEscapes && i+1 < len(sql) {
			i++
			continue
		}
		if sql[i] != quote {
			continue
		}
		if i+1 < len(sql) && sql[i+1] == quote {
			i++
			continue
		}

		return i + 1
	}

	return len(sql)
}

func hasEscapeStringPrefix(sql string, quoteIndex int) bool {
	if sql[quoteIndex] != '\'' || quoteIndex == 0 || sql[quoteIndex-1] != 'E' && sql[quoteIndex-1] != 'e' {
		return false
	}

	return quoteIndex == 1 || !isSQLWordByte(sql[quoteIndex-2])
}

func dollarQuoteEnd(sql string, start int) int {
	tagEnd := start + 1
	for tagEnd < len(sql) && isDollarTagByte(sql[tagEnd]) {
		tagEnd++
	}
	if tagEnd >= len(sql) || sql[tagEnd] != '$' {
		return start
	}

	tag := sql[start : tagEnd+1]
	if end := strings.Index(sql[tagEnd+1:], tag); end >= 0 {
		return tagEnd + 1 + end + len(tag)
	}

	return len(sql)
}

func isSQLWordByte(char byte) bool {
	return char >= 'A' && char <= 'Z' || char >= 'a' && char <= 'z' || char >= '0' && char <= '9' || char == '_'
}

func isDollarTagByte(char byte) bool {
	return isSQLWordByte(char)
}

func (db *pgxDB) setLockID(ctx context.Context) error {
	q := "SELECT CURRENT_DATABASE(), CURRENT_SCHEMA()"

	var database, schema string

	if err := db.conn.QueryRow(ctx, q).Scan(&database, &schema); err != nil {
		return fmt.Errorf("query row: %w", err)
	}

	tableParts := strings.Split(db.tableLockName, ".")
	relation := tableParts[len(tableParts)-1]
	if len(tableParts) == 2 {
		schema = tableParts[0]
	}

	db.table = pgx.Identifier{schema, relation}.Sanitize()
	identity := strings.Join([]string{database, schema, relation}, "\x00")
	hash := fnv.New64a()
	_, _ = hash.Write([]byte(identity))

	db.lockID = int64(hash.Sum64())

	return nil
}

func newPgxConn(conn *pgx.Conn) pgxConn {
	return conn
}

func newPgxPoolConn(conn *pgxpool.Conn) pgxConn {
	return conn
}
