package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/rinat1313/zakupki-collector/internal/model"
	"github.com/rinat1313/zakupki-collector/internal/store"
)

type Server struct {
	store *store.Store
	mux   *http.ServeMux
}

func New(st *store.Store) *Server {
	s := &Server{store: st, mux: http.NewServeMux()}
	s.routes()
	return s
}

func (s *Server) Handler() http.Handler {
	return s.mux
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /healthz", s.health)
	s.mux.HandleFunc("GET /tenders/search", s.searchDescription)
	s.mux.HandleFunc("GET /tenders", s.listOrSearch)
	s.mux.HandleFunc("POST /tenders", s.create)
	s.mux.HandleFunc("GET /tenders/{number}", s.getExact)
	s.mux.HandleFunc("PUT /tenders/{number}", s.update)
	s.mux.HandleFunc("DELETE /tenders/{number}", s.delete)
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// GET /tenders?number_like=&q=&exclude=&law=&limit=&offset=
func (s *Server) listOrSearch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := model.SearchFilter{
		PurchaseNumberLike: q.Get("number_like"),
		Law:                q.Get("law"),
		Limit:              atoiDefault(q.Get("limit"), 100),
		Offset:             atoiDefault(q.Get("offset"), 0),
	}
	if include := q.Get("q"); include != "" {
		f.DescriptionInclude = splitWords(include)
	}
	if exclude := q.Get("exclude"); exclude != "" {
		f.DescriptionExclude = splitWords(exclude)
	}
	items, err := s.store.Search(r.Context(), f)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if items == nil {
		items = []model.Tender{}
	}
	writeJSON(w, http.StatusOK, items)
}

// GET /tenders/search?include=слово1,слово2&exclude=слово3&law=
func (s *Server) searchDescription(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := model.SearchFilter{
		DescriptionInclude: splitWords(q.Get("include")),
		DescriptionExclude: splitWords(q.Get("exclude")),
		Law:                q.Get("law"),
		Limit:              atoiDefault(q.Get("limit"), 100),
		Offset:             atoiDefault(q.Get("offset"), 0),
	}
	if f.DescriptionInclude == nil && q.Get("q") != "" {
		f.DescriptionInclude = splitWords(q.Get("q"))
	}
	items, err := s.store.Search(r.Context(), f)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if items == nil {
		items = []model.Tender{}
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) getExact(w http.ResponseWriter, r *http.Request) {
	number := r.PathValue("number")
	t, err := s.store.GetByPurchaseNumber(r.Context(), number)
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, t)
}

func (s *Server) create(w http.ResponseWriter, r *http.Request) {
	var body tenderDTO
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	t, err := body.toModel()
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if t.PurchaseNumber == "" {
		writeErr(w, http.StatusBadRequest, errors.New("purchase_number required"))
		return
	}
	if err := s.store.Create(r.Context(), t); err != nil {
		writeErr(w, http.StatusConflict, err)
		return
	}
	writeJSON(w, http.StatusCreated, t)
}

func (s *Server) update(w http.ResponseWriter, r *http.Request) {
	number := r.PathValue("number")
	var body tenderDTO
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	t, err := body.toModel()
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	t.PurchaseNumber = number
	if err := s.store.Update(r.Context(), t); err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, http.StatusOK, t)
}

func (s *Server) delete(w http.ResponseWriter, r *http.Request) {
	number := r.PathValue("number")
	if err := s.store.DeleteByPurchaseNumber(r.Context(), number); err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type tenderDTO struct {
	PurchaseNumber string  `json:"purchase_number"`
	Description    string  `json:"description"`
	Customer       string  `json:"customer"`
	CustomerINN    string  `json:"customer_inn"`
	NMCK           *string `json:"nmck"`
	EndDate        *string `json:"end_date"`
	LastUpdatedAt  *string `json:"last_updated_at"`
	Law            string  `json:"law"`
	DocumentType   string  `json:"document_type"`
}

func (d tenderDTO) toModel() (*model.Tender, error) {
	t := &model.Tender{
		PurchaseNumber: d.PurchaseNumber,
		Description:    d.Description,
		Customer:       d.Customer,
		CustomerINN:    d.CustomerINN,
		Law:            d.Law,
		DocumentType:   d.DocumentType,
		RawSource:      "api",
	}
	if d.NMCK != nil && *d.NMCK != "" {
		v, err := decimal.NewFromString(*d.NMCK)
		if err != nil {
			return nil, err
		}
		t.NMCK = &v
	}
	if d.EndDate != nil && *d.EndDate != "" {
		tm, err := time.Parse(time.RFC3339, *d.EndDate)
		if err != nil {
			return nil, err
		}
		t.EndDate = &tm
	}
	if d.LastUpdatedAt != nil && *d.LastUpdatedAt != "" {
		tm, err := time.Parse(time.RFC3339, *d.LastUpdatedAt)
		if err != nil {
			return nil, err
		}
		t.LastUpdatedAt = &tm
	}
	return t, nil
}

func splitWords(s string) []string {
	parts := strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == ';' || r == '|'
	})
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func atoiDefault(s string, def int) int {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return n
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, err error) {
	writeJSON(w, code, map[string]string{"error": err.Error()})
}
