package database

import (
	"context"
	"errors"
	"fmt"

	"github.com/LSariol/Cove/internal/tokens"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// The methods in this file implement tokens.Store on cove.tokens and
// cove.token_log.

var _ tokens.Store = (*Database)(nil)

// classifyToken replaces driver errors with tokens.ErrNotFound or
// tokens.ErrExists, and returns every other error unchanged.
func classifyToken(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return tokens.ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
		return tokens.ErrExists
	}
	return err
}

// tokenColumns is the column list scanned by scanToken, in order.
const tokenColumns = `id, name, read_patterns, write_patterns, created_at, rotated_at, last_used_at`

func scanToken(row pgx.Row) (tokens.Token, error) {
	var t tokens.Token
	err := row.Scan(&t.ID, &t.Name, &t.Read, &t.Write, &t.CreatedAt, &t.RotatedAt, &t.LastUsedAt)
	return t, err
}

// WithinTokenTx runs fn in a transaction (see WithinTx).
func (d *Database) WithinTokenTx(ctx context.Context, fn func(tx tokens.Store) error) error {
	return d.WithinTx(ctx, func(tx Store) error {
		return fn(tx.(*Database))
	})
}

func (d *Database) CreateToken(ctx context.Context, name string, hash []byte, read []string, write []string) (tokens.Token, error) {
	const query = `
	INSERT INTO cove.tokens (name, token_hash, read_patterns, write_patterns)
	VALUES ($1, $2, $3, $4)
	RETURNING ` + tokenColumns

	// A nil slice would be sent as NULL; the columns want an empty array.
	if read == nil {
		read = []string{}
	}
	if write == nil {
		write = []string{}
	}

	t, err := scanToken(d.conn().QueryRow(ctx, query, name, hash, read, write))
	if err != nil {
		return t, fmt.Errorf("create token %q: %w", name, classifyToken(err))
	}
	return t, nil
}

func (d *Database) TokenByHash(ctx context.Context, hash []byte) (tokens.Token, error) {
	query := `SELECT ` + tokenColumns + ` FROM cove.tokens WHERE token_hash = $1`

	t, err := scanToken(d.conn().QueryRow(ctx, query, hash))
	if err != nil {
		return t, fmt.Errorf("look up token: %w", classifyToken(err))
	}
	return t, nil
}

func (d *Database) TokenByName(ctx context.Context, name string) (tokens.Token, error) {
	query := `SELECT ` + tokenColumns + ` FROM cove.tokens WHERE name = $1`

	t, err := scanToken(d.conn().QueryRow(ctx, query, name))
	if err != nil {
		return t, fmt.Errorf("token %q: %w", name, classifyToken(err))
	}
	return t, nil
}

func (d *Database) ListTokens(ctx context.Context) ([]tokens.Token, error) {
	rows, err := d.conn().Query(ctx, `SELECT `+tokenColumns+` FROM cove.tokens ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("list tokens: %w", err)
	}
	defer rows.Close()

	var list []tokens.Token
	for rows.Next() {
		t, err := scanToken(rows)
		if err != nil {
			return nil, fmt.Errorf("scan token: %w", err)
		}
		list = append(list, t)
	}
	return list, rows.Err()
}

// tokenExists turns "no row changed" into tokens.ErrNotFound when the token
// doesn't exist, or nil when it does (the change just wasn't needed).
func (d *Database) tokenExists(ctx context.Context, name string) error {
	var exists bool
	err := d.conn().QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM cove.tokens WHERE name = $1)`, name).Scan(&exists)
	if err != nil {
		return fmt.Errorf("token %q: %w", name, err)
	}
	if !exists {
		return fmt.Errorf("token %q: %w", name, tokens.ErrNotFound)
	}
	return nil
}

func (d *Database) AddTokenPattern(ctx context.Context, name string, pattern string, write bool) (bool, error) {
	column := "read_patterns"
	if write {
		column = "write_patterns"
	}
	query := `
	UPDATE cove.tokens
	SET ` + column + ` = array_append(` + column + `, $2)
	WHERE name = $1 AND NOT ($2 = ANY(` + column + `))`

	tag, err := d.conn().Exec(ctx, query, name, pattern)
	if err != nil {
		return false, fmt.Errorf("add %q to token %q: %w", pattern, name, err)
	}
	if tag.RowsAffected() == 0 {
		return false, d.tokenExists(ctx, name)
	}
	return true, nil
}

func (d *Database) RemoveTokenPattern(ctx context.Context, name string, pattern string) (bool, error) {
	const query = `
	UPDATE cove.tokens
	SET read_patterns = array_remove(read_patterns, $2),
	    write_patterns = array_remove(write_patterns, $2)
	WHERE name = $1 AND ($2 = ANY(read_patterns) OR $2 = ANY(write_patterns))`

	tag, err := d.conn().Exec(ctx, query, name, pattern)
	if err != nil {
		return false, fmt.Errorf("remove %q from token %q: %w", pattern, name, err)
	}
	if tag.RowsAffected() == 0 {
		return false, d.tokenExists(ctx, name)
	}
	return true, nil
}

func (d *Database) ReplaceTokenPattern(ctx context.Context, oldPattern string, newPattern string) ([]string, error) {
	const query = `
	UPDATE cove.tokens
	SET read_patterns = array_replace(read_patterns, $1, $2),
	    write_patterns = array_replace(write_patterns, $1, $2)
	WHERE $1 = ANY(read_patterns) OR $1 = ANY(write_patterns)
	RETURNING name`

	rows, err := d.conn().Query(ctx, query, oldPattern, newPattern)
	if err != nil {
		return nil, fmt.Errorf("replace %q in tokens: %w", oldPattern, err)
	}
	names, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("replace %q in tokens: %w", oldPattern, err)
	}
	return names, nil
}

func (d *Database) SetTokenHash(ctx context.Context, name string, hash []byte) error {
	const query = `UPDATE cove.tokens SET token_hash = $2, rotated_at = now() WHERE name = $1`

	tag, err := d.conn().Exec(ctx, query, name, hash)
	if err != nil {
		return fmt.Errorf("rotate token %q: %w", name, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("rotate token %q: %w", name, tokens.ErrNotFound)
	}
	return nil
}

func (d *Database) DeleteToken(ctx context.Context, name string) error {
	tag, err := d.conn().Exec(ctx, `DELETE FROM cove.tokens WHERE name = $1`, name)
	if err != nil {
		return fmt.Errorf("revoke token %q: %w", name, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("revoke token %q: %w", name, tokens.ErrNotFound)
	}
	return nil
}

// TouchToken records that a token was used. It writes at most once a minute
// per token, so busy projects don't cause a write per request.
func (d *Database) TouchToken(ctx context.Context, id int64) error {
	const query = `
	UPDATE cove.tokens SET last_used_at = now()
	WHERE id = $1 AND (last_used_at IS NULL OR last_used_at < now() - interval '1 minute')`

	if _, err := d.conn().Exec(ctx, query, id); err != nil {
		return fmt.Errorf("touch token: %w", err)
	}
	return nil
}

func (d *Database) LogTokenEvent(ctx context.Context, e tokens.Event) error {
	const query = `
	INSERT INTO cove.token_log (token_name, action, detail, source)
	VALUES ($1, $2, NULLIF($3, ''), $4)`

	if _, err := d.conn().Exec(ctx, query, e.TokenName, e.Action, e.Detail, e.Source); err != nil {
		return fmt.Errorf("log token event: %w", err)
	}
	return nil
}

// ListTokenEvents returns the named token's events, newest first. limit <= 0
// returns them all.
func (d *Database) ListTokenEvents(ctx context.Context, name string, limit int) ([]tokens.Event, error) {
	query := `
	SELECT token_name, action, COALESCE(detail, ''), source, occurred_at
	FROM cove.token_log
	WHERE token_name = $1
	ORDER BY occurred_at DESC, id DESC`
	args := []any{name}
	if limit > 0 {
		query += ` LIMIT $2`
		args = append(args, limit)
	}

	rows, err := d.conn().Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("token history for %q: %w", name, err)
	}
	defer rows.Close()

	var events []tokens.Event
	for rows.Next() {
		var e tokens.Event
		if err := rows.Scan(&e.TokenName, &e.Action, &e.Detail, &e.Source, &e.OccurredAt); err != nil {
			return nil, fmt.Errorf("scan token event: %w", err)
		}
		events = append(events, e)
	}
	return events, rows.Err()
}
