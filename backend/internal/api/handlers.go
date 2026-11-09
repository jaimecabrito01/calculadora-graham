package api

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"calculadora-graham/internal/brapi"
	"calculadora-graham/internal/graham"
	"calculadora-graham/internal/store"
)

// FonteCVM expõe o subconjunto do store usado pelo fallback CVM no handler.
// Interface para testabilidade (fake no unit test, store real em produção).
type FonteCVM interface {
	BuscaMapping(ctx context.Context, ticker string) (store.Mapping, bool, error)
	BuscaFundamentos(ctx context.Context, cnpj string) (store.FundamentosRow, bool, error)
}

type Server struct {
	brapi *brapi.Client
	db    FonteCVM // nil quando DATABASE_URL ausente (degrada para brapi only)
}

func New(b *brapi.Client) *Server { return &Server{brapi: b} }

// novoComStore cria o server com fallback CVM (fundamentos do sync).
func novoComStore(b *brapi.Client, db *store.Store) *Server { return &Server{brapi: b, db: db} }

var tickerRe = regexp.MustCompile(`^[A-Z0-9]{1,10}$`)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("erro ao serializar resposta: %v", err)
	}
}

type erroRes struct {
	Erro   string `json:"erro"`
	Ticker string `json:"ticker,omitempty"`
}

func writeErro(w http.ResponseWriter, status int, msg, ticker string) {
	writeJSON(w, status, erroRes{Erro: msg, Ticker: ticker})
}

func normalizeTicker(raw string) string {
	return strings.ToUpper(strings.TrimSpace(raw))
}

// Ativos: GET /api/ativos?search={termo}&limit={n} — autocomplete a partir da brapi.
func (s *Server) Ativos(w http.ResponseWriter, r *http.Request) {
	limit := 20
	if l := r.URL.Query().Get("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 100 {
			limit = n
		}
	}
	resp, err := s.brapi.Tickers(r.Context(), r.URL.Query().Get("search"), limit)
	if err != nil {
		responderErro(w, err, "", "falha ao buscar ativos")
		return
	}
	ativos := make([]map[string]any, 0, len(resp.Results))
	for _, it := range resp.Results {
		ativo := map[string]any{
			"ticker": it.Symbol,
			"nome":   it.Name,
		}
		if it.LongName != nil {
			ativo["long_name"] = *it.LongName
		}
		if it.Logo != nil && *it.Logo != "" {
			ativo["logo_url"] = *it.Logo
		}
		ativos = append(ativos, ativo)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ativos":      ativos,
		"total":       resp.Pagination.TotalItems,
		"tem_proxima": resp.Pagination.HasNext,
	})
}

// Graham: GET /api/graham/{ticker} — calcula preço justo pelo método Graham.
// VPA: preferência para bookValue da brapi (quando o plano/liberação permite);
// senão, CVM: VPA = PL conta 2.03 (controlador puro) ÷ (ações totais − tesouraria).
func (s *Server) Graham(w http.ResponseWriter, r *http.Request) {
	ticker := normalizeTicker(r.PathValue("ticker"))
	if ticker == "" || !tickerRe.MatchString(ticker) {
		writeErro(w, http.StatusBadRequest, "ticker inválido (maiúsculas, sem sufixo — ex.: PETR4)", ticker)
		return
	}
	br, err := s.brapi.Quote(r.Context(), ticker)
	if err != nil {
		responderErro(w, err, ticker, "falha ao buscar dados do ativo")
		return
	}
	nome := br.LongName
	if nome == "" {
		nome = br.ShortName
	}
	res := graham.Calculo{Ticker: br.Symbol, Nome: nome}
	if br.Logo != nil {
		res.Logo = *br.Logo
	}

	switch {
	case br.DefaultKeyStats.BookValue != nil && *br.DefaultKeyStats.BookValue > 0:
		res.VpaFonte = "brapi"
	case s.db != nil:
		// caminho CVM: mapping confirmado → fundamentos → VPA calculado
		m, okM, err := s.db.BuscaMapping(r.Context(), ticker)
		if err != nil {
			log.Printf("busca mapping %s: %v", ticker, err)
			writeErro(w, http.StatusBadGateway, "falha interna ao buscar mapeamento", ticker)
			return
		}
		if !okM {
			writeErro(w, http.StatusUnprocessableEntity,
				"VPA indisponível: plano brapi não inclui fundamentos e não há mapeamento confirmado ticker→CNPJ (rode o sync ou promova com cmd/sync -promote TICKER=CNPJ)", ticker)
			return
		}
		f, okF, err := s.db.BuscaFundamentos(r.Context(), m.CNPJ)
		if err != nil {
			log.Printf("busca fundamentos %s: %v", ticker, err)
			writeErro(w, http.StatusBadGateway, "falha interna ao buscar fundamentos CVM", ticker)
			return
		}
		if !okF || f.PL <= 0 {
			writeErro(w, http.StatusUnprocessableEntity, "fundamentos CVM não disponíveis para o CNPJ mapeado; rode o sync", ticker)
			return
		}
		fator := f.FatorAcoes
		if fator <= 0 {
			fator = 1
		}
		acoes := (f.AcoesTotal - f.AcoesTesouraria) * fator
		if acoes <= 0 {
			writeErro(w, http.StatusUnprocessableEntity, "quantidade de ações inválida na CVM para este ativo", ticker)
			return
		}
		vpa := f.PL / acoes // PL controlador puro (conta 2.03)
		res.VpaFonte, res.VpaReferencia = "cvm", f.DataFimExerc
		br.DefaultKeyStats.BookValue = &vpa
	default:
		writeErro(w, http.StatusUnprocessableEntity,
			"VPA indisponível: plano brapi não inclui fundamentos e o modo CVM está desativado (configure DATABASE_URL e rode o sync)", ticker)
		return
	}

	calc, err := graham.Calcular(br.Symbol, nome, &br.Price, br.EPS, br.DefaultKeyStats.BookValue)
	if err != nil {
		var inap *graham.Inaplicavel
		if errors.As(err, &inap) {
			writeErro(w, http.StatusUnprocessableEntity, inap.Razao, ticker)
			return
		}
		writeErro(w, http.StatusBadGateway, "falha ao calcular", ticker)
		return
	}
	calc.Logo, calc.VpaFonte, calc.VpaReferencia = res.Logo, res.VpaFonte, res.VpaReferencia
	writeJSON(w, http.StatusOK, calc)
}

func responderErro(w http.ResponseWriter, err error, ticker, contexto string) {
	var he *brapi.HTTPError
	switch {
	case errors.Is(err, brapi.ErrNotFound):
		writeErro(w, http.StatusNotFound, "ativo não encontrado na brapi", ticker)
	case errors.As(err, &he):
		if he.Status == http.StatusUnauthorized || he.Status == http.StatusForbidden {
			// transparência: front entende que é chave/plan, não nosso bug
			writeErro(w, he.Status, he.Msg, ticker)
		} else if he.Status == http.StatusTooManyRequests {
			writeErro(w, http.StatusTooManyRequests, he.Msg, ticker)
		} else {
			log.Printf("%s: %v", contexto, err)
			writeErro(w, http.StatusBadGateway, he.Msg, ticker)
		}
	default:
		log.Printf("%s: %v", contexto, err)
		writeErro(w, http.StatusBadGateway, "falha ao consultar a fonte de dados", ticker)
	}
}

func Healthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}
