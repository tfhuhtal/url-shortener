package shortener

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const schemaSQL = `
CREATE TABLE IF NOT EXISTS urls (
	id BIGSERIAL PRIMARY KEY,
	short_code VARCHAR(16) NOT NULL UNIQUE,
	long_url TEXT NOT NULL UNIQUE,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
`

type URLRecord struct {
	ShortCode string
	LongURL   string
	CreatedAt time.Time
}

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) EnsureSchema(ctx context.Context) error {
	_, err := r.pool.Exec(ctx, schemaSQL)
	return err
}

func (r *Repository) CreateOrGet(ctx context.Context, shortCode, longURL string) (URLRecord, error) {
	const query = `
		INSERT INTO urls(short_code, long_url)
		VALUES ($1, $2)
		ON CONFLICT (long_url) DO UPDATE SET long_url = EXCLUDED.long_url
		RETURNING short_code, long_url, created_at;
	`

	var record URLRecord
	err := r.pool.QueryRow(ctx, query, shortCode, longURL).Scan(&record.ShortCode, &record.LongURL, &record.CreatedAt)
	return record, err
}

func (r *Repository) GetByShortCode(ctx context.Context, shortCode string) (URLRecord, error) {
	const query = `
		SELECT short_code, long_url, created_at
		FROM urls
		WHERE short_code = $1;
	`

	var record URLRecord
	err := r.pool.QueryRow(ctx, query, shortCode).Scan(&record.ShortCode, &record.LongURL, &record.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return URLRecord{}, ErrNotFound
	}

	return record, err
}

func IsShortCodeConflict(err error) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}

	return pgErr.Code == "23505" && pgErr.ConstraintName == "urls_short_code_key"
}
