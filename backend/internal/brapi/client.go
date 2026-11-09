package brapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

var ErrNotFound = errors.New("ticker não encontrado")

type HTTPError struct {
	Status int
	Msg    string
}

func (e *HTTPError) Error() string { return e.Msg }

type Client struct {
	baseURL    string
	apiKey     string
	http       *http.Client
	maxRetries int
}

func NewClient(baseURL, apiKey string, timeout time.Duration, maxRetries int) *Client {
	return &Client{
		baseURL:    strings.TrimSuffix(baseURL, "/"),
		apiKey:     apiKey,
		http:       &http.Client{Timeout: timeout},
		maxRetries: maxRetries,
	}
}

type KeyStatistics struct {
	BookValue   *float64 `json:"bookValue"`
	PriceToBook *float64 `json:"priceToBook"`
}

type QuoteResult struct {
	Symbol          string        `json:"symbol"`
	ShortName       string        `json:"shortName"`
	LongName        string        `json:"longName"`
	Currency        string        `json:"currency"`
	Price           float64       `json:"regularMarketPrice"`
	MarketCap       *float64      `json:"marketCap"`
	EPS             *float64      `json:"earningsPerShare"`
	Logo            *string       `json:"logourl"`
	DefaultKeyStats KeyStatistics `json:"defaultKeyStatistics"`
}

type QuoteResponse struct {
	Results []QuoteResult `json:"results"`
}

type TickerItem struct {
	Symbol   string              `json:"symbol"`
	Name     string              `json:"name"`
	LongName *string             `json:"longName"`
	Logo     *string             `json:"logoUrl"`
	Type     *string             `json:"assetType"`
	Quote    *TickerQuoteSummary `json:"quote"`
}

// TickerQuoteSummary é o resumo de cotação por item em /v2/tickers.
type TickerQuoteSummary struct {
	LastPrice *float64 `json:"lastPrice"`
	MarketCap *float64 `json:"marketCap"`
}

type Pagination struct {
	Page       int  `json:"page"`
	Limit      int  `json:"limit"`
	TotalItems int  `json:"totalItems"`
	TotalPages int  `json:"totalPages"`
	HasNext    bool `json:"hasNextPage"`
}

type TickersResponse struct {
	Results    []TickerItem `json:"results"`
	Pagination Pagination   `json:"pagination"`
}

const quoteModules = "defaultKeyStatistics,financialData"

// Quote busca cotação + fundamentos (LPA, VPA) de um ticker.
// Se a requisição com moduleslarar voltar 403 (fora do plano), refaz sem modules:
// preço, LPA e logo continuam disponíveis para todos os tickers da cotação simples.
func (c *Client) Quote(ctx context.Context, symbol string) (*QuoteResult, error) {
	retorna, err := c.quoteParse(ctx, fmt.Sprintf("%s/quote/%s?modules=%s", c.baseURL, url.PathEscape(symbol), quoteModules))
	if err == nil {
		return retorna, nil
	}
	var he *HTTPError
	if errors.As(err, &he) && he.Status == http.StatusForbidden {
		log.Printf("brapi: %s sem modules (403) — refazendo cotação simples", symbol)
		return c.quoteParse(ctx, fmt.Sprintf("%s/quote/%s", c.baseURL, url.PathEscape(symbol)))
	}
	return nil, err
}

func (c *Client) quoteParse(ctx context.Context, u string) (*QuoteResult, error) {
	body, err := c.get(ctx, u)
	if err != nil {
		return nil, err
	}
	var resp QuoteResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, &HTTPError{Status: http.StatusBadGateway, Msg: "resposta inválida da brapi Quote"}
	}
	if len(resp.Results) == 0 {
		return nil, ErrNotFound
	}
	return &resp.Results[0], nil
}

// Tickers busca ativos (autocomplete) em /v2/tickers, restrito a ações.
func (c *Client) Tickers(ctx context.Context, search string, limit int) (*TickersResponse, error) {
	q := url.Values{}
	if search != "" {
		q.Set("search", search)
	}
	q.Set("type", "stock")
	q.Set("limit", strconv.Itoa(limit))
	u := fmt.Sprintf("%s/v2/tickers?%s", c.baseURL, q.Encode())
	body, err := c.get(ctx, u)
	if err != nil {
		return nil, err
	}
	var resp TickersResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, &HTTPError{Status: http.StatusBadGateway, Msg: "resposta inválida da brapi Tickers"}
	}
	return &resp, nil
}

// TodaLista devolve todos os tickers de ações (type=stock) paginando até o fim.
// Usada pelo ETL de sugestões; tolera página com erro (para e devolve o que já tem).
func (c *Client) TodaLista(ctx context.Context) ([]TickerItem, error) {
	var todas []TickerItem
	for page := 1; page <= 100; page++ {
		q := url.Values{}
		q.Set("type", "stock")
		q.Set("page", strconv.Itoa(page))
		q.Set("limit", "100")
		u := fmt.Sprintf("%s/v2/tickers?%s", c.baseURL, q.Encode())
		body, err := c.get(ctx, u)
		if err != nil {
			if len(todas) > 0 {
				return todas, nil // devolve o que conseguiu
			}
			return nil, err
		}
		var resp TickersResponse
		if err := json.Unmarshal(body, &resp); err != nil {
			if len(todas) > 0 {
				return todas, nil
			}
			return nil, &HTTPError{Status: http.StatusBadGateway, Msg: "resposta inválida da brapi Tickers"}
		}
		todas = append(todas, resp.Results...)
		if resp.Pagination.TotalItems != 0 && len(todas) >= resp.Pagination.TotalItems {
			break
		}
		if len(resp.Results) == 0 {
			break
		}
	}
	return todas, nil
}

func (c *Client) get(ctx context.Context, rawURL string) ([]byte, error) {
	resp, err := c.doGet(ctx, rawURL)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusTooManyRequests && c.maxRetries > 0 {
		resp.Body.Close()
		delay := 2 * time.Second
		if ra := resp.Header.Get("Retry-After"); ra != "" {
			if secs, perr := strconv.Atoi(ra); perr == nil {
				delay = time.Duration(secs) * time.Second
			}
		}
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		resp, err = c.doGet(ctx, rawURL)
		if err != nil {
			return nil, err
		}
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, &HTTPError{Status: http.StatusBadGateway, Msg: "falha ao ler resposta da brapi"}
	}
	switch resp.StatusCode {
	case http.StatusOK:
		return body, nil
	case http.StatusNotFound:
		return nil, ErrNotFound
	case http.StatusUnauthorized:
		return nil, &HTTPError{Status: resp.StatusCode, Msg: "chave da brapi ausente ou inválida"}
	case http.StatusForbidden:
		return nil, &HTTPError{Status: resp.StatusCode, Msg: "o plano da chave da brapi não inclui este recurso (fundamentos/modules)"}
	case http.StatusTooManyRequests:
		return nil, &HTTPError{Status: resp.StatusCode, Msg: "cota da brapi esgotada (429)"}
	default:
		return nil, &HTTPError{
			Status: http.StatusBadGateway,
			Msg:    fmt.Sprintf("brapi respondeu status %d", resp.StatusCode),
		}
	}
}

func (c *Client) doGet(ctx context.Context, rawURL string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, &HTTPError{Status: http.StatusInternalServerError, Msg: "falha ao montar requisição"}
	}
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, &HTTPError{Status: http.StatusBadGateway, Msg: "falha ao chamar a brapi"}
	}
	return resp, nil
}
