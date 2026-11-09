package store

import (
	"context"
	"os"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Mapping struct {
	Ticker string
	CNPJ   string
	Fonte  string // "b3" | "manual"
}

type FundamentosRow struct {
	CNPJ            string
	FonteDoc        string // "dfp" | "itr"
	DataReferencia  string // DT_REFER (YYYY-MM-DD)
	DataFimExerc    string // DT_FIM_EXERC
	PL              float64
	AcoesTotal      float64
	AcoesTesouraria float64
	Denom           string
	FatorAcoes      float64 // 1 = contagem crua; 1000 = emissor reportou em milhares
}

type Store struct {
	pool *pgxpool.Pool
}

var schemaSQL = `
CREATE TABLE IF NOT EXISTS ticker_mapping (
	ticker        TEXT PRIMARY KEY,
	cnpj          TEXT NOT NULL,
	fonte         TEXT NOT NULL,
	atualizado_em TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS mapping_sugestoes (
	ticker        TEXT NOT NULL,
	cnpj          TEXT NOT NULL,
	diff_pct      REAL NOT NULL,
	criado_em     TIMESTAMPTZ NOT NULL DEFAULT now(),
	PRIMARY KEY (ticker, cnpj)
);
	CREATE TABLE IF NOT EXISTS fundamentos (
	cnpj             TEXT NOT NULL,
	fonte_doc        TEXT NOT NULL,
	dt_refer         TEXT NOT NULL,
	dt_fim_exerc     TEXT NOT NULL,
	pl               DOUBLE PRECISION,
	acoes_total      DOUBLE PRECISION,
	acoes_tesouraria DOUBLE PRECISION,
	denom            TEXT,
	PRIMARY KEY (cnpj, fonte_doc, dt_refer)
);
ALTER TABLE fundamentos ADD COLUMN IF NOT EXISTS denom TEXT;
ALTER TABLE fundamentos ADD COLUMN IF NOT EXISTS fator_acoes DOUBLE PRECISION NOT NULL DEFAULT 1;

`

func Open(ctx context.Context, databaseURL string) (*Store, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	if _, err := pool.Exec(ctx, schemaSQL); err != nil {
		pool.Close()
		return nil, err
	}
	return &Store{pool: pool}, nil
}

func OpenFromEnv(ctx context.Context) (*Store, error) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		return nil, errConfig("DATABASE_URL não configurada")
	}
	return Open(ctx, url)
}

type configError string

func (e configError) Error() string { return string(e) }

func errConfig(msg string) error { return configError(msg) }

// Close encerra o pool. Idempotente.
func (s *Store) Close() {
	if s != nil && s.pool != nil {
		s.pool.Close()
	}
}

// --- ticker_mapping ---

func (s *Store) UpsertMappingConfirmado(ctx context.Context, m Mapping) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO ticker_mapping (ticker, cnpj, fonte, atualizado_em)
		VALUES ($1, $2, $3, now())
		ON CONFLICT (ticker) DO UPDATE SET cnpj = $2, fonte = $3, atualizado_em = now()
	`, m.Ticker, m.CNPJ, m.Fonte)
	return err
}

func (s *Store) BuscaMapping(ctx context.Context, ticker string) (Mapping, bool, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT ticker, cnpj, fonte FROM ticker_mapping WHERE ticker = $1
	`, ticker)
	var m Mapping
	if err := row.Scan(&m.Ticker, &m.CNPJ, &m.Fonte); err != nil {
		if err == pgx.ErrNoRows {
			return Mapping{}, false, nil
		}
		return Mapping{}, false, err
	}
	return m, true, nil
}

func (s *Store) MapeamentosConfirmados(ctx context.Context) ([]Mapping, error) {
	rows, err := s.pool.Query(ctx, `SELECT ticker, cnpj, fonte FROM ticker_mapping`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Mapping{}
	for rows.Next() {
		var m Mapping
		if err := rows.Scan(&m.Ticker, &m.CNPJ, &m.Fonte); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// DeleteMapping remove um mapeamento manualmente corrigido.
func (s *Store) DeleteMapping(ctx context.Context, ticker string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM ticker_mapping WHERE ticker = $1`, ticker)
	return err
}

// --- mapping_sugestoes (nunca servidas pelo runtime) ---

func (s *Store) UpsertSugestao(ctx context.Context, ticker, cnpj string, diffPct float64) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO mapping_sugestoes (ticker, cnpj, diff_pct, criado_em)
		VALUES ($1, $2, $3, now())
		ON CONFLICT (ticker, cnpj) DO UPDATE SET diff_pct = $3, criado_em = now()
	`, ticker, cnpj, diffPct)
	return err
}

// ClearSugestoes limpa sugestões do ciclo anterior antes de gerar as novas.
func (s *Store) ClearSugestoes(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM mapping_sugestoes`)
	return err
}

type Sugestao struct {
	Ticker  string
	CNPJ    string
	DiffPct float64
}

func (s *Store) ListaSugestoes(ctx context.Context) ([]Sugestao, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT ticker, cnpj, diff_pct FROM mapping_sugestoes ORDER BY diff_pct ASC, ticker ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Sugestao{}
	for rows.Next() {
		var sg Sugestao
		if err := rows.Scan(&sg.Ticker, &sg.CNPJ, &sg.DiffPct); err != nil {
			return nil, err
		}
		out = append(out, sg)
	}
	return out, rows.Err()
}

// --- fundamentos ---

func (s *Store) UpsertFundamentos(ctx context.Context, r FundamentosRow) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO fundamentos (cnpj, fonte_doc, dt_refer, dt_fim_exerc, pl, acoes_total, acoes_tesouraria, denom)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (cnpj, fonte_doc, dt_refer) DO UPDATE SET
			dt_fim_exerc = $4, pl = $5, acoes_total = $6, acoes_tesouraria = $7, denom = $8
	`, r.CNPJ, r.FonteDoc, r.DataReferencia, r.DataFimExerc, r.PL, r.AcoesTotal, r.AcoesTesouraria, r.Denom)
	return err
}

// BuscaFundamentos devolve a linha mais recente de DFP e de ITR; o chamador decide.
func (s *Store) BuscaFundamentos(ctx context.Context, cnpj string) (FundamentosRow, bool, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT cnpj, fonte_doc, dt_refer, dt_fim_exerc, pl, acoes_total, acoes_tesouraria, fator_acoes
		FROM fundamentos
		WHERE cnpj = $1
		ORDER BY dt_refer DESC, fonte_doc DESC
		LIMIT 1
	`, cnpj)
	var r FundamentosRow
	if err := row.Scan(&r.CNPJ, &r.FonteDoc, &r.DataReferencia, &r.DataFimExerc, &r.PL, &r.AcoesTotal, &r.AcoesTesouraria, &r.FatorAcoes); err != nil {
		if err == pgx.ErrNoRows {
			return FundamentosRow{}, false, nil
		}
		return FundamentosRow{}, false, err
	}
	return r, true, nil
}

// SetFatorAcoes grava o fator de escala da contagem de ações de um CNPJ
// (o composicao_capital da CVM não informa unidade; ver pacote mapping).
func (s *Store) SetFatorAcoes(ctx context.Context, cnpj string, fator float64) error {
	_, err := s.pool.Exec(ctx, `UPDATE fundamentos SET fator_acoes = $2 WHERE cnpj = $1`, cnpj, fator)
	return err
}

// SincronizadoRecentemente sinaliza se já há fundamentos com dt_refer recente no banco,
// para o API decidir se precisa disparar sync no boot.
func (s *Store) SincronizadoRecentemente(ctx context.Context) (bool, error) {
	var count int64
	err := s.pool.QueryRow(ctx, `
		SELECT count(*) FROM fundamentos
		WHERE dt_refer >= $1
	`, time.Now().AddDate(-1, 0, 0).Format("2006-01-02")).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}
