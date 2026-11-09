package b3

import (
	"strings"
	"testing"
)

// linha gera um registro 01 com os campos variáveis; posição 1-based do layout.
func linhaCotahist(ticker, nomres, tpmerc, bdi string) string {
	var b strings.Builder
	b.WriteString("01")
	b.WriteString("20260115")      // DATPRE
	b.WriteString(bdi)             // CODBDI (2)
	b.WriteString(pad(ticker, 12)) // CODNEG
	b.WriteString(tpmerc)          // TPMERC (3)
	b.WriteString(pad(nomres, 12)) // NOMRES
	b.WriteString(strings.Repeat(" ", 246))
	return b.String()
}

func pad(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s + strings.Repeat(" ", n-len(s))
}

func TestParseCotahist(t *testing.T) {
	in := strings.Join([]string{
		strings.Repeat("0", 245), // registro garbage, length je
		linhaCotahist("PETR4", "PETROBRAS", "010", "02"),
		linhaCotahist("WEGE3", "WEG", "010", "02"),
		linhaCotahist("PETR4F", "PETROBRAS", "020", "96"), // fracionário
		linhaCotahist("PETRE34", "OPCAO", "070", "02"),    // opção: fora
		linhaCotahist("MERCADO", "TERMO", "010", "10"),    // bdi recibo: fora
		"99COTAHIST.A2026",
	}, "\n")

	out, err := parseCotahist(strings.NewReader(in))
	if err != nil {
		t.Fatalf("parseCotahist: %v", err)
	}
	if len(out) != 3 {
		t.Fatalf("esperado 3 tickers, veio %d: %v", len(out), out)
	}
	if out["PETR4"].Nome != "PETROBRAS" || out["WEGE3"].Nome != "WEG" {
		t.Errorf("nomes: %v %v", out["PETR4"], out["WEGE3"])
	}
	if out["PETR4"].Nome == "" || out["PETR4"].Pregao != "20260115" {
		t.Errorf("pregão: %q", out["PETR4"].Pregao)
	}
}
