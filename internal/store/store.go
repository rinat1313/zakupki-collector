package store

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/rinat1313/zakupki-collector/internal/model"
)

//go:embed schema.sql
var schemaFS embed.FS

// Store — модуль взаимодействия с PostgreSQL.
type Store struct {
	pool *pgxpool.Pool
}

func New(ctx context.Context, databaseURL string) (*Store, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("connect postgres: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	s := &Store{pool: pool}
	if err := s.Migrate(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() {
	s.pool.Close()
}

func (s *Store) Migrate(ctx context.Context) error {
	// pg_trgm нужен для приближённого поиска
	if _, err := s.pool.Exec(ctx, `CREATE EXTENSION IF NOT EXISTS pg_trgm`); err != nil {
		return fmt.Errorf("enable pg_trgm: %w", err)
	}
	sqlBytes, err := schemaFS.ReadFile("schema.sql")
	if err != nil {
		return err
	}
	if _, err := s.pool.Exec(ctx, string(sqlBytes)); err != nil {
		return fmt.Errorf("apply schema: %w", err)
	}
	return nil
}

func (s *Store) Create(ctx context.Context, t *model.Tender) error {
	const q = `
INSERT INTO tenders (
  purchase_number, description, customer, customer_inn, nmck,
  end_date, last_updated_at, law, document_type, raw_source
) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
RETURNING id, created_at, updated_at`
	return s.pool.QueryRow(ctx, q,
		t.PurchaseNumber, t.Description, t.Customer, t.CustomerINN, decimalOrNil(t.NMCK),
		t.EndDate, t.LastUpdatedAt, nullLaw(t.Law), t.DocumentType, t.RawSource,
	).Scan(&t.ID, &t.CreatedAt, &t.UpdatedAt)
}

func (s *Store) GetByID(ctx context.Context, id int64) (*model.Tender, error) {
	return s.scanOne(ctx, `SELECT `+tenderColumns+` FROM tenders WHERE id = $1`, id)
}

// GetByPurchaseNumber — строгое соответствие номеру закупки.
func (s *Store) GetByPurchaseNumber(ctx context.Context, number string) (*model.Tender, error) {
	return s.scanOne(ctx, `SELECT `+tenderColumns+` FROM tenders WHERE purchase_number = $1`, number)
}

func (s *Store) Update(ctx context.Context, t *model.Tender) error {
	const q = `
UPDATE tenders SET
  description = $2,
  customer = $3,
  customer_inn = $4,
  nmck = $5,
  end_date = $6,
  last_updated_at = $7,
  law = $8,
  document_type = $9,
  raw_source = $10,
  updated_at = NOW()
WHERE purchase_number = $1
RETURNING id, created_at, updated_at`
	err := s.pool.QueryRow(ctx, q,
		t.PurchaseNumber, t.Description, t.Customer, t.CustomerINN, decimalOrNil(t.NMCK),
		t.EndDate, t.LastUpdatedAt, nullLaw(t.Law), t.DocumentType, t.RawSource,
	).Scan(&t.ID, &t.CreatedAt, &t.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("tender %q not found", t.PurchaseNumber)
	}
	return err
}

func (s *Store) DeleteByPurchaseNumber(ctx context.Context, number string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM tenders WHERE purchase_number = $1`, number)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("tender %q not found", number)
	}
	return nil
}

func (s *Store) DeleteByID(ctx context.Context, id int64) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM tenders WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("tender id=%d not found", id)
	}
	return nil
}

// Upsert — вставка/обновление. Если last_updated_at не изменился — пропускаем.
func (s *Store) Upsert(ctx context.Context, t *model.Tender) (model.UpsertResult, error) {
	existing, err := s.GetByPurchaseNumber(ctx, t.PurchaseNumber)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return "", err
	}
	if existing == nil {
		if err := s.Create(ctx, t); err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "23505" {
				// гонка: повторяем как update/skip
				return s.Upsert(ctx, t)
			}
			return "", err
		}
		return model.UpsertInserted, nil
	}

	if sameTimestamp(existing.LastUpdatedAt, t.LastUpdatedAt) {
		*t = *existing
		return model.UpsertSkipped, nil
	}

	t.ID = existing.ID
	if err := s.Update(ctx, t); err != nil {
		return "", err
	}
	return model.UpsertUpdated, nil
}

// Search — поиск: точный номер / похожий номер / текст в описании с исключениями.
func (s *Store) Search(ctx context.Context, f model.SearchFilter) ([]model.Tender, error) {
	var (
		b    strings.Builder
		args []any
		n    = 1
	)
	b.WriteString(`SELECT ` + tenderColumns + ` FROM tenders WHERE 1=1`)

	if f.ExactPurchaseNumber != "" {
		b.WriteString(fmt.Sprintf(` AND purchase_number = $%d`, n))
		args = append(args, f.ExactPurchaseNumber)
		n++
	}
	if f.PurchaseNumberLike != "" {
		b.WriteString(fmt.Sprintf(` AND purchase_number ILIKE $%d`, n))
		args = append(args, "%"+f.PurchaseNumberLike+"%")
		n++
	}
	for _, word := range f.DescriptionInclude {
		word = strings.TrimSpace(word)
		if word == "" {
			continue
		}
		b.WriteString(fmt.Sprintf(` AND description ILIKE $%d`, n))
		args = append(args, "%"+word+"%")
		n++
	}
	for _, word := range f.DescriptionExclude {
		word = strings.TrimSpace(word)
		if word == "" {
			continue
		}
		b.WriteString(fmt.Sprintf(` AND description NOT ILIKE $%d`, n))
		args = append(args, "%"+word+"%")
		n++
	}
	if f.Law != "" {
		b.WriteString(fmt.Sprintf(` AND law = $%d`, n))
		args = append(args, f.Law)
		n++
	}

	b.WriteString(` ORDER BY last_updated_at DESC NULLS LAST, id DESC`)

	limit := f.Limit
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	b.WriteString(fmt.Sprintf(` LIMIT $%d`, n))
	args = append(args, limit)
	n++
	if f.Offset > 0 {
		b.WriteString(fmt.Sprintf(` OFFSET $%d`, n))
		args = append(args, f.Offset)
	}

	rows, err := s.pool.Query(ctx, b.String(), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []model.Tender
	for rows.Next() {
		t, err := scanTender(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *t)
	}
	return out, rows.Err()
}

var ErrNotFound = errors.New("not found")

const tenderColumns = `
id, purchase_number, description, customer, customer_inn, nmck,
end_date, last_updated_at, law, document_type, raw_source, created_at, updated_at`

type rowScanner interface {
	Scan(dest ...any) error
}

func (s *Store) scanOne(ctx context.Context, q string, args ...any) (*model.Tender, error) {
	row := s.pool.QueryRow(ctx, q, args...)
	t, err := scanTender(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return t, err
}

func scanTender(row rowScanner) (*model.Tender, error) {
	var (
		t       model.Tender
		nmck    *decimal.Decimal
		end     *time.Time
		updated *time.Time
	)
	err := row.Scan(
		&t.ID, &t.PurchaseNumber, &t.Description, &t.Customer, &t.CustomerINN, &nmck,
		&end, &updated, &t.Law, &t.DocumentType, &t.RawSource, &t.CreatedAt, &t.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	t.NMCK = nmck
	t.EndDate = end
	t.LastUpdatedAt = updated
	return &t, nil
}

func decimalOrNil(d *decimal.Decimal) any {
	if d == nil {
		return nil
	}
	return *d
}

func nullLaw(law string) string {
	if law == "" {
		return "44"
	}
	return law
}

func sameTimestamp(a, b *time.Time) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return a.Equal(*b)
}
