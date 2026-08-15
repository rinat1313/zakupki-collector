package model

import (
	"time"

	"github.com/shopspring/decimal"
)

// Tender — нормализованная запись о закупке для хранения в PostgreSQL.
type Tender struct {
	ID              int64            `json:"id"`
	PurchaseNumber  string           `json:"purchase_number"`
	Description     string           `json:"description"`
	Customer        string           `json:"customer"`
	CustomerINN     string           `json:"customer_inn"`
	NMCK            *decimal.Decimal `json:"nmck,omitempty"`
	EndDate         *time.Time       `json:"end_date,omitempty"`
	LastUpdatedAt   *time.Time       `json:"last_updated_at,omitempty"`
	Law             string           `json:"law"` // "44" | "223"
	DocumentType    string           `json:"document_type,omitempty"`
	RawSource       string           `json:"raw_source,omitempty"`
	CreatedAt       time.Time        `json:"created_at"`
	UpdatedAt       time.Time        `json:"updated_at"`
}

// SearchFilter — параметры поиска закупок.
type SearchFilter struct {
	// ExactPurchaseNumber — строгое соответствие номеру закупки.
	ExactPurchaseNumber string
	// PurchaseNumberLike — приближённый поиск по номеру (ILIKE %...%).
	PurchaseNumberLike string
	// DescriptionInclude — слова/фразы, которые должны встречаться в описании (AND).
	DescriptionInclude []string
	// DescriptionExclude — слова/фразы-исключения: тендеры с ними отбрасываются.
	DescriptionExclude []string
	// Law — фильтр по закону ("44", "223"), пусто = любой.
	Law string
	// Limit / Offset — пагинация.
	Limit  int
	Offset int
}

// UpsertResult — результат идемпотентной записи.
type UpsertResult string

const (
	UpsertInserted UpsertResult = "inserted"
	UpsertUpdated  UpsertResult = "updated"
	UpsertSkipped  UpsertResult = "skipped" // last_updated_at не изменился
)
