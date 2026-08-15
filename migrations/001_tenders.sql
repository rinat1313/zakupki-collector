-- tenders: нормализованные закупки из ЕИС
CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE TABLE IF NOT EXISTS tenders (
    id                BIGSERIAL PRIMARY KEY,
    purchase_number   TEXT NOT NULL,
    description       TEXT NOT NULL DEFAULT '',
    customer          TEXT NOT NULL DEFAULT '',
    customer_inn      TEXT NOT NULL DEFAULT '',
    nmck              NUMERIC(20, 2),
    end_date          TIMESTAMPTZ,
    last_updated_at   TIMESTAMPTZ,
    law               TEXT NOT NULL DEFAULT '44',
    document_type     TEXT NOT NULL DEFAULT '',
    raw_source        TEXT NOT NULL DEFAULT '',
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT tenders_purchase_number_uniq UNIQUE (purchase_number)
);

CREATE INDEX IF NOT EXISTS tenders_purchase_number_trgm_idx
    ON tenders USING gin (purchase_number gin_trgm_ops);

CREATE INDEX IF NOT EXISTS tenders_description_trgm_idx
    ON tenders USING gin (description gin_trgm_ops);

CREATE INDEX IF NOT EXISTS tenders_last_updated_at_idx
    ON tenders (last_updated_at DESC NULLS LAST);

CREATE INDEX IF NOT EXISTS tenders_customer_inn_idx
    ON tenders (customer_inn);
