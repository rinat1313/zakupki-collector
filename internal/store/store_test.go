package store_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/rinat1313/zakupki-collector/internal/model"
	"github.com/rinat1313/zakupki-collector/internal/store"
)

func testStore(t *testing.T) *store.Store {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set")
	}
	ctx := context.Background()
	st, err := store.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	return st
}

func TestUpsertSkipUnchanged(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()

	updated := time.Date(2026, 8, 15, 10, 0, 0, 0, time.UTC)
	nmck := decimal.RequireFromString("1000.00")
	tr := &model.Tender{
		PurchaseNumber: "TEST-UPSERT-001",
		Description:    "Поставка бумаги офисной",
		Customer:       "ООО Тест",
		CustomerINN:    "7700000000",
		NMCK:           &nmck,
		LastUpdatedAt:  &updated,
		Law:            "44",
	}
	_ = st.DeleteByPurchaseNumber(ctx, tr.PurchaseNumber)

	res, err := st.Upsert(ctx, tr)
	if err != nil || res != model.UpsertInserted {
		t.Fatalf("insert: res=%s err=%v", res, err)
	}

	res, err = st.Upsert(ctx, tr)
	if err != nil || res != model.UpsertSkipped {
		t.Fatalf("skip: res=%s err=%v", res, err)
	}

	newer := updated.Add(time.Minute)
	tr.Description = "Поставка бумаги офисной обновлено"
	tr.LastUpdatedAt = &newer
	res, err = st.Upsert(ctx, tr)
	if err != nil || res != model.UpsertUpdated {
		t.Fatalf("update: res=%s err=%v", res, err)
	}

	got, err := st.GetByPurchaseNumber(ctx, "TEST-UPSERT-001")
	if err != nil {
		t.Fatal(err)
	}
	if got.Description != "Поставка бумаги офисной обновлено" {
		t.Fatalf("desc=%q", got.Description)
	}
	_ = st.DeleteByPurchaseNumber(ctx, tr.PurchaseNumber)
}

func TestSearchIncludeExclude(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()

	seed := []model.Tender{
		{PurchaseNumber: "SEARCH-001", Description: "закупка бумаги А4", Customer: "A", Law: "44"},
		{PurchaseNumber: "SEARCH-002", Description: "закупка мебели офисной", Customer: "B", Law: "44"},
		{PurchaseNumber: "SEARCH-003", Description: "бумага и картриджи исключение тест", Customer: "C", Law: "44"},
	}
	for i := range seed {
		_ = st.DeleteByPurchaseNumber(ctx, seed[i].PurchaseNumber)
		if err := st.Create(ctx, &seed[i]); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		for _, s := range seed {
			_ = st.DeleteByPurchaseNumber(ctx, s.PurchaseNumber)
		}
	})

	exact, err := st.GetByPurchaseNumber(ctx, "SEARCH-001")
	if err != nil || exact.PurchaseNumber != "SEARCH-001" {
		t.Fatalf("exact: %v %#v", err, exact)
	}

	like, err := st.Search(ctx, model.SearchFilter{PurchaseNumberLike: "SEARCH-00", Limit: 10})
	if err != nil || len(like) < 3 {
		t.Fatalf("like: n=%d err=%v", len(like), err)
	}

	found, err := st.Search(ctx, model.SearchFilter{
		DescriptionInclude: []string{"бумаг"},
		DescriptionExclude: []string{"исключение"},
		Limit:              10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 1 || found[0].PurchaseNumber != "SEARCH-001" {
		t.Fatalf("include/exclude got %#v", found)
	}
}
