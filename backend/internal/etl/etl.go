// Package etl orquestra a sincronização de fundamentos:
//  1. CVM (DFP ano anterior + atual, ITR ano atual) → PL e ações por CNPJ → fundamentos
//  2. B3 COTAHIST (best effort) → nome do emissor por ticker
//  3. brapi /v2/tickers → todos os tickers de ações com marketCap e preço
//  4. mapeamento ticker→CNPJ: confirmação automática exige evidência nominal
//     (nome B3/brapi ↔ DENOM_CIA) + numérica (marketCap÷preço ≈ ações CVM);
//     numérica sozinha vira apenas sugestão (melhor casamento), nunca uso em produção.
package etl

import (
	"context"
	"log"
	"time"

	"calculadora-graham/internal/b3"
	"calculadora-graham/internal/brapi"
	"calculadora-graham/internal/cvm"
	"calculadora-graham/internal/mapping"
	"calculadora-graham/internal/store"
)

type Sync struct {
	db    *store.Store
	cvm   *cvm.Client
	b3    *b3.Client
	brapi *brapi.Client

	semB3        bool
	semSugestoes bool
}

func Novo(db *store.Store, cvmClient *cvm.Client, b3Client *b3.Client, brapiClient *brapi.Client) *Sync {
	return &Sync{db: db, cvm: cvmClient, b3: b3Client, brapi: brapiClient}
}

// SemB3 desativa a tentativa B3 (ex.: proxy restrito).
func (s *Sync) SemB3(v bool) *Sync { s.semB3 = v; return s }

// SemSugestoes desativa a geração de sugestões numéricas (sync só de fundamentos).
func (s *Sync) SemSugestoes(v bool) *Sync { s.semSugestoes = v; return s }

// empresaSnapshot é a visão deduplicada por CNPJ para o casamento.
type empresaSnapshot struct {
	dtRef string
	denom string
	total float64
	tes   float64
}

func dedupeEmpresas(regs []cvm.Fundamentos) map[string]empresaSnapshot {
	m := map[string]empresaSnapshot{}
	for _, r := range regs {
		if r.DataReferencia == "" || r.AcoesTotal <= 0 {
			continue
		}
		cur, ok := m[r.CNPJ]
		if !ok || (r.DataReferencia != cur.dtRef && r.DataReferencia > cur.dtRef) {
			m[r.CNPJ] = empresaSnapshot{
				dtRef: r.DataReferencia, denom: r.Denom,
				total: r.AcoesTotal, tes: r.AcoesTesouraria,
			}
		}
	}
	return m
}

// Rodar executa o pipeline completo; erros de fonte são registrados e não abortam.
func (s *Sync) Rodar(ctx context.Context) error {
	log.Printf("etl: sync CVM (DFP último ano + atual, ITR atual)")

	regs, erros := s.cvm.Sincroniza(ctx, time.Now())
	if ctx.Err() != nil {
		return ctx.Err()
	}
	for _, e := range erros {
		log.Printf("etl: aviso CVM: %v", e)
	}
	if len(regs) == 0 {
		log.Printf("etl: nenhum fundamento CVM obtido — mapeamento ficará sem dados")
	}
	gravados := 0
	for _, r := range regs {
		err := s.db.UpsertFundamentos(ctx, store.FundamentosRow{
			CNPJ:            r.CNPJ,
			FonteDoc:        r.Fonte,
			DataReferencia:  r.DataReferencia,
			DataFimExerc:    r.DataFimExerc,
			PL:              r.PL,
			AcoesTotal:      r.AcoesTotal,
			AcoesTesouraria: r.AcoesTesouraria,
			Denom:           r.Denom,
		})
		if err != nil {
			return err
		}
		gravados++
	}
	log.Printf("etl: %d fundamentos gravados no PG", gravados)

	if s.semSugestoes {
		log.Printf("etl: sync concluído (sem etapa de sugestões)")
		return nil
	}

	empresas := dedupeEmpresas(regs)
	log.Printf("etl: %d empresas CVM com PL+ações", len(empresas))

	nomesB3 := map[string]string{}
	if !s.semB3 {
		emis, err := s.b3.UltimosEmissores(ctx, time.Now())
		if err != nil {
			log.Printf("etl: aviso B3: %v", err)
		}
		for t, e := range emis {
			nomesB3[t] = e.Nome
		}
		log.Printf("etl: %d tickers com nome via B3", len(nomesB3))
	}

	tickers, err := s.brapi.TodaLista(ctx)
	if err != nil {
		log.Printf("etl: aviso brapi lista completa: %v", err)
	}
	log.Printf("etl: %d tickers brapi recebidos", len(tickers))

	if err := s.db.ClearSugestoes(ctx); err != nil {
		return err
	}

	confirmados, sugeridos := 0, 0
	fatores := map[string]float64{}
	for _, it := range tickers {
		if it.Quote == nil || it.Quote.LastPrice == nil || it.Quote.MarketCap == nil {
			continue
		}
		if *it.Quote.LastPrice <= 0 || *it.Quote.MarketCap <= 0 {
			continue
		}
		est := *it.Quote.MarketCap / *it.Quote.LastPrice
		nomeFonte := nomesB3[it.Symbol]
		if nomeFonte == "" && it.LongName != nil {
			nomeFonte = *it.LongName
		}
		if nomeFonte == "" {
			continue
		}
		// avalia contra todas as empresas CVM; confirma tudo que casar
		// nome+numérica; caso contrário guarda a melhor sugestão numérica
		melhorCnpj, melhorDiff := "", 0.0
		temConfirmado := false
		for cnpj, emp := range empresas {
			conf, diff, fator := mapping.Avalia(mapping.Feeds{
				Ticker:      it.Symbol,
				NomeDoAtivo: nomeFonte,
				DenomCVM:    emp.denom,
				AcoesEst:    est,
				AcoesTotal:  emp.total,
				AcoesNet:    emp.total - emp.tes,
			})
			if conf {
				if err := s.db.UpsertMappingConfirmado(ctx, store.Mapping{
					Ticker: it.Symbol, CNPJ: cnpj, Fonte: "auto",
				}); err != nil {
					return err
				}
				fatores[cnpj] = fator
				temConfirmado = true
				confirmados++
				continue
			}
			if melhorCnpj == "" || diff < melhorDiff {
				melhorCnpj, melhorDiff = cnpj, diff
			}
		}
		if temConfirmado {
			continue
		}
		if melhorCnpj != "" && melhorDiff <= mapping.ToleranciaPct {
			if err := s.db.UpsertSugestao(ctx, it.Symbol, melhorCnpj, melhorDiff); err != nil {
				return err
			}
			sugeridos++
		}
	}
	// persiste o fator de escala por CNPJ (aplica a todas as linhas DFP/ITR); 1 quando cru
	escalados := 0
	for cnpj, fator := range fatores {
		if err := s.db.SetFatorAcoes(ctx, cnpj, fator); err != nil {
			return err
		}
		if fator != 1 {
			escalados++
		}
	}
	log.Printf("etl: mapping: %d confirmados (auto nome+numérica), %d CNPJs reescalados em milhares, %d sugestões numéricas registradas",
		confirmados, escalados, sugeridos)
	return nil
}
