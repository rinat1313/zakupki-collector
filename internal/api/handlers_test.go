package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/rinat1313/zakupki-collector/internal/api"
	"github.com/rinat1313/zakupki-collector/internal/store"
)

func TestAPI_CRUDAndSearch(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set")
	}
	ctx := context.Background()
	st, err := store.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	h := api.New(st).Handler()
	number := "API-TEST-" + time.Now().Format("150405")

	body := map[string]any{
		"purchase_number": number,
		"description":     "поставка серверов и комплектующих",
		"customer":        "ООО API",
		"customer_inn":    "7711111111",
		"nmck":            "9999.99",
		"law":             "44",
	}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/tenders", bytes.NewReader(b))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", rr.Code, rr.Body.String())
	}
	t.Cleanup(func() {
		req := httptest.NewRequest(http.MethodDelete, "/tenders/"+number, nil)
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
	})

	req = httptest.NewRequest(http.MethodGet, "/tenders/"+number, nil)
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("get status=%d", rr.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/tenders/search?include=сервер&exclude=мебел", nil)
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("search status=%d body=%s", rr.Code, rr.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/tenders?number_like=API-TEST", nil)
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("like status=%d", rr.Code)
	}
}
