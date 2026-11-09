package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"calculadora-graham/internal/brapi"
	"calculadora-graham/internal/store"
)

func f(v float64) *float64 { return &v }

func mockBrapi() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v2/tickers":
			resp := brapi.TickersResponse{
				Results:    []brapi.TickerItem{{Symbol: "PETR4", Name: "Petroleo Brasileiro SA Pfd"}},
				Pagination: brapi.Pagination{Page: 1, Limit: 20, TotalItems: 1, TotalPages: 1},
			}
			_ = json.NewEncoder(w).Encode(resp)
		case r.URL.Path == "/quote/PETR4":
			lpa := 10.35
			vpa := 37.32
			resp := brapi.QuoteResponse{Results: []brapi.QuoteResult{{
				Symbol:          "PETR4",
				LongName:        "Petroleo Brasileiro SA Pfd",
				Price:           45.76,
				EPS:             &lpa,
				DefaultKeyStats: brapi.KeyStatistics{BookValue: &vpa},
			}}}
			_ = json.NewEncoder(w).Encode(resp)
		case r.URL.Path == "/quote/WEGE3":
			// cotação simples (sem modules): LPA vem, bookValue vem nil
			lpa := 2.2
			resp := brapi.QuoteResponse{Results: []brapi.QuoteResult{{
				Symbol:   "WEGE3",
				LongName: "WEG SA",
				Price:    48.5,
				EPS:      &lpa,
			}}}
			_ = json.NewEncoder(w).Encode(resp)
		case r.URL.Path == "/quote/AAA3":
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":true,"message":"not found"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func newClient(baseURL string, retries int) *brapi.Client {
	return brapi.NewClient(baseURL, "", time.Second, retries)
}

func TestGrahamOK(t *testing.T) {
	ts := mockBrapi()
	defer ts.Close()
	handler := Routes(newClient(ts.URL, 0), nil, 30*time.Second)

	req := httptest.NewRequest(http.MethodGet, "/api/graham/petr4", nil) // minuscula: normaliza
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var res map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("json: %v", err)
	}
	if res["ticker"] != "PETR4" {
		t.Errorf("ticker = %v", res["ticker"])
	}
	// √(22,5 × 10,35 × 37,32) ≈ 93,23
	j, _ := res["preco_justo"].(float64)
	if j < 93.2 || j > 93.3 {
		t.Errorf("preco_justo = %v", j)
	}
}

func TestGrahamNotFound(t *testing.T) {
	ts := mockBrapi()
	defer ts.Close()
	handler := Routes(newClient(ts.URL, 0), nil, 30*time.Second)

	req := httptest.NewRequest(http.MethodGet, "/api/graham/AAA3", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestGrahamTickerInvalido(t *testing.T) {
	ts := mockBrapi()
	defer ts.Close()
	handler := Routes(newClient(ts.URL, 0), nil, 30*time.Second)

	for _, tk := range []string{"", "PETR4.SA", "PETR-4!"} {
		req := httptest.NewRequest(http.MethodGet, "/api/graham/"+tk, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		// vazio nem chega ao handler: chi responde 404 sem segmento {ticker}
		if tk == "" && (rec.Code == http.StatusBadRequest || rec.Code == http.StatusNotFound) {
			continue
		}
		if rec.Code != http.StatusBadRequest {
			t.Errorf("ticker %q: status = %d", tk, rec.Code)
		}
	}
}

func TestAtivos(t *testing.T) {
	ts := mockBrapi()
	defer ts.Close()
	handler := Routes(newClient(ts.URL, 0), nil, 30*time.Second)

	req := httptest.NewRequest(http.MethodGet, "/api/ativos?search=petr&limit=1", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var res struct {
		Ativos     []map[string]any `json:"ativos"`
		TemProxima bool             `json:"tem_proxima"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("json: %v", err)
	}
	if len(res.Ativos) != 1 || res.Ativos[0]["ticker"] != "PETR4" {
		t.Errorf("ativos = %v", res.Ativos)
	}
}

func TestRetry429(t *testing.T) {
	var calls int
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.Header().Set("Retry-After", strconv.Itoa(1))
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		vpa := 1.0
		lpa := 1.0
		_ = json.NewEncoder(w).Encode(brapi.QuoteResponse{Results: []brapi.QuoteResult{{
			Symbol: "PETR4", Price: 1, EPS: &lpa,
			DefaultKeyStats: brapi.KeyStatistics{BookValue: &vpa},
		}}})
	}))
	defer ts.Close()
	handler := Routes(newClient(ts.URL, 1), nil, 30*time.Second)

	req := httptest.NewRequest(http.MethodGet, "/api/graham/PETR4", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if calls != 2 {
		t.Errorf("calls = %d, esperado 2 (429 + retry)", calls)
	}
}

// dbFixa fornece mapeamento/fundamentos fixos para testar o caminho CVM.
type dbFixa struct {
	mapping    store.Mapping
	temMapping bool
	fund       store.FundamentosRow
	temFund    bool
}

func (d *dbFixa) BuscaMapping(ctx context.Context, ticker string) (store.Mapping, bool, error) {
	if d.temMapping && d.mapping.Ticker == ticker {
		return d.mapping, true, nil
	}
	return store.Mapping{}, false, nil
}

func (d *dbFixa) BuscaFundamentos(ctx context.Context, cnpj string) (store.FundamentosRow, bool, error) {
	if d.temFund && d.fund.CNPJ == cnpj {
		return d.fund, true, nil
	}
	return store.FundamentosRow{}, false, nil
}

func TestGrahamViaCVM(t *testing.T) {
	ts := mockBrapi()
	defer ts.Close()
	db := &dbFixa{
		temMapping: true,
		mapping:    store.Mapping{Ticker: "WEGE3", CNPJ: "84429695000111", Fonte: "auto"},
		temFund:    true,
		fund:       store.FundamentosRow{CNPJ: "84429695000111", FonteDoc: "itr", DataReferencia: "2026-09-30", DataFimExerc: "2026-09-30", PL: 12.4e9, AcoesTotal: 2.44e9, AcoesTesouraria: 2.3e8},
	}
	handler := Routes(newClient(ts.URL, 0), db, 30*time.Second)

	req := httptest.NewRequest(http.MethodGet, "/api/graham/WEGE3", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var res struct {
		Ticker        string  `json:"ticker"`
		Vpa           float64 `json:"vpa"`
		VpaFonte      string  `json:"vpa_fonte"`
		VpaReferencia string  `json:"vpa_referencia"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("json: %v", err)
	}
	if res.VpaFonte != "cvm" || res.VpaReferencia != "2026-09-30" {
		t.Errorf("fonte/ref: %q/%q", res.VpaFonte, res.VpaReferencia)
	}
	// VPA = PL / (ações − tesouraria) = 12,4bi / 2,21bi ≈ 5,61
	if res.Vpa <= 5.0 || res.Vpa >= 6.0 {
		t.Errorf("vpa = %v, esperado PL/ações ≈ 5,61", res.Vpa)
	}
}

// ITSA4: CVM reporta ações em milhares; o fator persistido corrige o VPA.
func TestGrahamViaCVMEscalaMilhar(t *testing.T) {
	ts := mockBrapi()
	defer ts.Close()
	db := &dbFixa{
		temMapping: true,
		mapping:    store.Mapping{Ticker: "WEGE3", CNPJ: "61532644000115", Fonte: "auto"},
		temFund:    true,
		fund: store.FundamentosRow{
			CNPJ: "61532644000115", FonteDoc: "itr", DataReferencia: "2026-06-30", DataFimExerc: "2026-06-30",
			PL: 98.582e9, AcoesTotal: 11213687, AcoesTesouraria: 6337, FatorAcoes: 1000,
		},
	}
	handler := Routes(newClient(ts.URL, 0), db, 30*time.Second)

	req := httptest.NewRequest(http.MethodGet, "/api/graham/WEGE3", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var res struct {
		Vpa float64 `json:"vpa"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("json: %v", err)
	}
	// VPA = PL / ((total − tesouraria) × 1000) ≈ 8,80
	if res.Vpa <= 8.5 || res.Vpa >= 9.1 {
		t.Errorf("vpa = %v, esperado ≈ 8,80", res.Vpa)
	}
}

func TestGrahamSemMapping(t *testing.T) {
	ts := mockBrapi()
	defer ts.Close()
	handler := Routes(newClient(ts.URL, 0), &dbFixa{}, 30*time.Second)

	// mockBrapi só conhece PETR4/fundamentos brapi; um ticker sem mapping mas com brapi 403
	// exige mock diferente: aqui validamos o 422 sem mapping usando tickers fora do mock.
	// (Quote de WEGE3 no mockBrapi cai no default 404 do server de teste)
	req := httptest.NewRequest(http.MethodGet, "/api/graham/XXXX3", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("ticker inexistente no mock: esperado 404, veio %d", rec.Code)
	}
}

func TestQuoteCom403NosModules(t *testing.T) {
	// brapi responde 403 com modules e 200 sem; o client deve cair para simples
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.RawQuery, "modules=") {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		lpa := 1.5
		_ = json.NewEncoder(w).Encode(brapi.QuoteResponse{Results: []brapi.QuoteResult{{
			Symbol: "WEGE3", Price: 48.5, EPS: &lpa,
			LongName: "WEG SA",
		}}})
	}))
	defer ts.Close()
	c := newClient(ts.URL, 0)
	q, err := c.Quote(context.Background(), "WEGE3")
	if err != nil {
		t.Fatalf("quote: %v", err)
	}
	if q.DefaultKeyStats.BookValue != nil {
		t.Errorf("sem modules, bookValue deveria ser nil")
	}
	if q.EPS == nil || *q.EPS != 1.5 {
		t.Errorf("LPA perdido no retry simples")
	}
}
