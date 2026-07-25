package ledger

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"slices"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const migrationLockID int64 = 0x4355544c494e45

//go:embed migrations/*.sql
var migrationFiles embed.FS

type Migration struct {
	Version  string
	Checksum string
	SQL      string
}

func Migrations() ([]Migration, error) {
	names, err := fs.Glob(migrationFiles, "migrations/*.sql")
	if err != nil {
		return nil, fmt.Errorf("list migrations: %w", err)
	}
	slices.Sort(names)
	result := make([]Migration, 0, len(names))
	for _, name := range names {
		data, err := migrationFiles.ReadFile(name)
		if err != nil {
			return nil, fmt.Errorf("read migration %s: %w", name, err)
		}
		sum := sha256.Sum256(data)
		result = append(result, Migration{
			Version:  path.Base(name),
			Checksum: hex.EncodeToString(sum[:]),
			SQL:      string(data),
		})
	}
	return result, nil
}

func ApplyMigrations(ctx context.Context, pool *pgxpool.Pool) error {
	migrations, err := Migrations()
	if err != nil {
		return err
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin migration transaction: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck -- commit determines the transaction result

	if _, err := tx.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS cutline_schema_migrations (
			version TEXT PRIMARY KEY,
			checksum CHAR(64) NOT NULL CHECK (checksum ~ '^[0-9a-f]{64}$'),
			applied_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp()
		)`); err != nil {
		return fmt.Errorf("create migration ledger: %w", err)
	}
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", migrationLockID); err != nil {
		return fmt.Errorf("lock migrations: %w", err)
	}

	for _, migration := range migrations {
		var checksum string
		err := tx.QueryRow(ctx,
			"SELECT checksum FROM cutline_schema_migrations WHERE version = $1",
			migration.Version,
		).Scan(&checksum)
		switch {
		case err == nil:
			if checksum != migration.Checksum {
				return fmt.Errorf("migration %s checksum changed", migration.Version)
			}
			continue
		case !errors.Is(err, pgx.ErrNoRows):
			return fmt.Errorf("read migration %s: %w", migration.Version, err)
		}

		if _, err := tx.Conn().PgConn().Exec(ctx, migration.SQL).ReadAll(); err != nil {
			return fmt.Errorf("apply migration %s: %w", migration.Version, err)
		}
		if _, err := tx.Exec(ctx,
			"INSERT INTO cutline_schema_migrations (version, checksum) VALUES ($1, $2)",
			migration.Version,
			migration.Checksum,
		); err != nil {
			return fmt.Errorf("record migration %s: %w", migration.Version, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit migrations: %w", err)
	}
	return nil
}
