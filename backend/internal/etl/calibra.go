package etl

import (
	"context"
	"fmt"

	"calculadora-graham/internal/brapi"
	"calculadora-graham/internal/mapping"
	"calculadora-graham/internal/store"
)

// CalibraFator ajusta o fator de escala da contagem de ações de um CNPJ usando a
// cotação da brapi (marketCap ÷ preço) como evidência numérica. Usado no
// -promote: sem isso um emissor que reporta as ações em milhares (ex.: ITSA4)
// produziria VPA 1000× maior. Best effort: devolve 1 quando não há evidência.
func CalibraFator(ctx context.Context, db *store.Store, br *brapi.Client, ticker, cnpj string) (float64, error) {
	f, ok, err := db.BuscaFundamentos(ctx, cnpj)
	if err != nil {
		return 1, err
	}
	if !ok || f.AcoesTotal <= 0 {
		return 1, fmt.Errorf("sem fundamentos para o CNPJ %s", cnpj)
	}
	q, err := br.Quote(ctx, ticker)
	if err != nil {
		return 1, err
	}
	if q.MarketCap == nil || *q.MarketCap <= 0 || q.Price <= 0 {
		return 1, fmt.Errorf("cotação sem marketCap/preço para %s", ticker)
	}
	est := *q.MarketCap / q.Price
	diff, fator := mapping.MelhorFator(est, f.AcoesTotal, f.AcoesTotal-f.AcoesTesouraria)
	if diff > mapping.ToleranciaPct {
		return 1, fmt.Errorf("evidência numérica fora da tolerância (diff %.2f%%)", diff)
	}
	if err := db.SetFatorAcoes(ctx, cnpj, fator); err != nil {
		return 1, err
	}
	return fator, nil
}
