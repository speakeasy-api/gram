package bootstrap

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
)

// reconcile brings an existing database up to the embedded schema.
//
// The schema is applied with CREATE ... IF NOT EXISTS, which creates whatever
// is missing but never touches a table that already exists. So a database
// created by an older schema keeps its old columns forever, and the first
// query written against the new shape fails at runtime -- typically as a
// NOT NULL constraint violation on a column the code no longer sets, which
// says nothing about the actual cause.
//
// Rather than track a schema version (which only works if every change
// remembers to bump it), the expected shape is derived by applying the
// embedded schema to a scratch in-memory database and introspecting the
// result. schema.sql stays the single source of truth and this needs no
// maintenance when it changes.
//
// Handled: columns added, columns removed, indexes removed. That covers the
// changes a dev IdP realistically accumulates. A column whose type or
// nullability changed in place cannot be altered by SQLite without rebuilding
// the table, so that case reports what it found and asks for the database to
// be deleted -- recreating it costs a login.
func reconcile(ctx context.Context, db *sql.DB, logger *slog.Logger) error {
	want, err := expectedShape(ctx)
	if err != nil {
		return err
	}

	got, err := introspect(ctx, db)
	if err != nil {
		return fmt.Errorf("introspect existing database: %w", err)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin schema reconcile: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	changed := 0
	for name, wantTable := range want {
		gotTable, ok := got[name]
		if !ok {
			// Absent entirely -- the schema apply that follows creates it.
			continue
		}

		n, err := reconcileTable(ctx, tx, name, wantTable, gotTable, logger)
		if err != nil {
			return err
		}
		changed += n
	}

	if changed == 0 {
		return nil
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit schema reconcile: %w", err)
	}
	logger.InfoContext(ctx, "upgraded dev-idp schema in place", slog.Int("changes", changed))
	return nil
}

// reconcileTable aligns one existing table with its expected shape and
// returns how many statements it ran.
func reconcileTable(ctx context.Context, tx *sql.Tx, name string, want, got table, logger *slog.Logger) (int, error) {
	changed := 0

	// Indexes first: SQLite refuses to drop a column an index still
	// references, so a retired index has to go before its column can.
	for idx := range got.indexes {
		if _, keep := want.indexes[idx]; keep {
			continue
		}
		if _, err := tx.ExecContext(ctx, `DROP INDEX IF EXISTS `+quoteIdent(idx)); err != nil {
			return changed, fmt.Errorf("drop retired index %s: %w", idx, err)
		}
		logger.InfoContext(ctx, "dropped retired index", slog.String("index", idx))
		changed++
	}

	for _, col := range got.order {
		if _, keep := want.columns[col]; keep {
			continue
		}
		if _, err := tx.ExecContext(ctx,
			`ALTER TABLE `+quoteIdent(name)+` DROP COLUMN `+quoteIdent(col)); err != nil {
			return changed, fmt.Errorf("drop retired column %s.%s: %w", name, col, err)
		}
		logger.InfoContext(ctx, "dropped retired column",
			slog.String("table", name), slog.String("column", col))
		changed++
	}

	for _, col := range want.order {
		wantCol := want.columns[col]
		gotCol, exists := got.columns[col]
		if exists {
			if err := assertCompatible(name, wantCol, gotCol); err != nil {
				return changed, err
			}
			continue
		}
		def, err := addColumnDef(name, wantCol)
		if err != nil {
			return changed, err
		}
		if _, err := tx.ExecContext(ctx,
			`ALTER TABLE `+quoteIdent(name)+` ADD COLUMN `+def); err != nil {
			return changed, fmt.Errorf("add column %s.%s: %w", name, col, err)
		}
		logger.InfoContext(ctx, "added missing column",
			slog.String("table", name), slog.String("column", col))
		changed++
	}

	return changed, nil
}

// assertCompatible reports whether an existing column can stay as-is. SQLite
// cannot alter a column's type or nullability in place, so a mismatch needs
// the database rebuilt.
func assertCompatible(tableName string, want, got column) error {
	switch {
	case !strings.EqualFold(want.dataType, got.dataType):
		return rebuildRequired(tableName, want.name,
			fmt.Sprintf("type changed from %q to %q", got.dataType, want.dataType))
	case want.notNull != got.notNull:
		return rebuildRequired(tableName, want.name,
			fmt.Sprintf("NOT NULL changed from %t to %t", got.notNull, want.notNull))
	default:
		return nil
	}
}

func rebuildRequired(tableName, columnName, why string) error {
	return fmt.Errorf(
		"dev-idp schema drift on %s.%s (%s) needs a rebuild SQLite cannot do in place; "+
			"delete the dev-idp database to recreate it (rm -rf local/devidp), then log in again",
		tableName, columnName, why)
}

// addColumnDef renders the DDL fragment for ALTER TABLE ADD COLUMN. SQLite
// rejects a NOT NULL column without a constant default, because existing rows
// would have nothing to hold.
func addColumnDef(tableName string, c column) (string, error) {
	if c.notNull && !c.dflt.Valid {
		return "", rebuildRequired(tableName, c.name, "new NOT NULL column has no default")
	}

	var b strings.Builder
	b.WriteString(quoteIdent(c.name))
	if c.dataType != "" {
		b.WriteString(" " + c.dataType)
	}
	if c.notNull {
		b.WriteString(" NOT NULL")
	}
	if c.dflt.Valid {
		b.WriteString(" DEFAULT " + c.dflt.String)
	}
	return b.String(), nil
}

// column is one row of PRAGMA table_info.
type column struct {
	name     string
	dataType string
	notNull  bool
	dflt     sql.NullString
}

// table is a table's column set (with declaration order preserved for stable
// statement ordering) plus the names of its non-implicit indexes.
type table struct {
	columns map[string]column
	order   []string
	indexes map[string]struct{}
}

// expectedShape applies the embedded schema to a throwaway in-memory database
// and introspects it, so the expectation is whatever schema.sql actually
// produces rather than a hand-maintained description of it.
func expectedShape(ctx context.Context) (map[string]table, error) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		return nil, fmt.Errorf("open scratch sqlite for expected schema: %w", err)
	}
	db.SetMaxOpenConns(1)
	defer func() { _ = db.Close() }()

	if _, err := db.ExecContext(ctx, schemaSQL()); err != nil {
		return nil, fmt.Errorf("apply schema to scratch database: %w", err)
	}

	shape, err := introspect(ctx, db)
	if err != nil {
		return nil, fmt.Errorf("introspect expected schema: %w", err)
	}
	return shape, nil
}

func introspect(ctx context.Context, db *sql.DB) (map[string]table, error) {
	names, err := tableNames(ctx, db)
	if err != nil {
		return nil, err
	}

	out := make(map[string]table, len(names))
	for _, name := range names {
		cols, order, err := tableColumns(ctx, db, name)
		if err != nil {
			return nil, err
		}
		idx, err := tableIndexes(ctx, db, name)
		if err != nil {
			return nil, err
		}
		out[name] = table{columns: cols, order: order, indexes: idx}
	}
	return out, nil
}

func tableNames(ctx context.Context, db *sql.DB) ([]string, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("list tables: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, fmt.Errorf("scan table name: %w", err)
		}
		names = append(names, n)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate tables: %w", err)
	}
	return names, nil
}

func tableColumns(ctx context.Context, db *sql.DB, tableName string) (map[string]column, []string, error) {
	// PRAGMA arguments cannot be bound; tableName comes from sqlite_master.
	rows, err := db.QueryContext(ctx, `SELECT name, type, "notnull", dflt_value FROM pragma_table_info(?)`, tableName)
	if err != nil {
		return nil, nil, fmt.Errorf("read columns of %s: %w", tableName, err)
	}
	defer func() { _ = rows.Close() }()

	cols := map[string]column{}
	var order []string
	for rows.Next() {
		var c column
		if err := rows.Scan(&c.name, &c.dataType, &c.notNull, &c.dflt); err != nil {
			return nil, nil, fmt.Errorf("scan column of %s: %w", tableName, err)
		}
		cols[c.name] = c
		order = append(order, c.name)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("iterate columns of %s: %w", tableName, err)
	}
	return cols, order, nil
}

// tableIndexes lists explicitly declared indexes. Indexes SQLite creates for
// PRIMARY KEY / UNIQUE constraints have no SQL of their own and belong to the
// table definition, so they are not reconcilable on their own.
func tableIndexes(ctx context.Context, db *sql.DB, tableName string) (map[string]struct{}, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT name FROM sqlite_master WHERE type = 'index' AND tbl_name = ? AND sql IS NOT NULL ORDER BY name`,
		tableName)
	if err != nil {
		return nil, fmt.Errorf("read indexes of %s: %w", tableName, err)
	}
	defer func() { _ = rows.Close() }()

	out := map[string]struct{}{}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, fmt.Errorf("scan index of %s: %w", tableName, err)
		}
		out[n] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate indexes of %s: %w", tableName, err)
	}
	return out, nil
}

func quoteIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}
