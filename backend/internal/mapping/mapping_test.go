package mapping

import "testing"

func TestAvaliaControado(t *testing.T) {
	f := Feeds{
		Ticker:      "PETR4",
		NomeDoAtivo: "PETROBRAS", // B3 NOMRES
		DenomCVM:    "PETROLEO BRASILEIRO S.A. PETROBRAS",
		AcoesEst:    12.94e9, // marketCap ÷ preço (brapi)
		AcoesTotal:  13.044496930e9,
		AcoesNet:    12.888733e9,
	}
	confirmado, diff, _ := Avalia(f)
	if !confirmado {
		t.Errorf("esperado confirmado, diff %v", diff)
	}
	if diff > 10 {
		t.Errorf("diff fora da tolerância: %v", diff)
	}
}

func TestAvaliaSolicitaSemNome(t *testing.T) {
	f := Feeds{
		Ticker:     "XYZ",
		AcoesEst:   12.9e9,
		AcoesTotal: 13.0e9,
		AcoesNet:   12.9e9,
	}
	confirmado, _, _ := Avalia(f)
	if confirmado {
		t.Errorf("numérica sozinha não deve confirmar")
	}
}

func TestAvaliaSolicitaSemNumerica(t *testing.T) {
	f := Feeds{
		Ticker:      "XYZ",
		NomeDoAtivo: "PETROBRAS",
		DenomCVM:    "PETROLEO BRASILEIRO S.A. PETROBRAS",
		AcoesEst:    0, // sem cotação no /v2/tickers
		AcoesTotal:  13.0e9,
	}
	confirmado, _, _ := Avalia(f)
	if confirmado {
		t.Errorf("nome sozinho não deve confirmar")
	}
}

func TestAvaliaNaoCasaNomeDiferente(t *testing.T) {
	f := Feeds{
		Ticker:      "XYZ",
		NomeDoAtivo: "MAGALUD",
		DenomCVM:    "WEG S.A.",
		AcoesEst:    1e9,
		AcoesTotal:  1e9,
	}
	confirmado, _, _ := Avalia(f)
	if confirmado {
		t.Errorf("nomes díspares não devem casar")
	}
}

// Expressão real do ITSA4: a CVM reporta as ações em milhares.
func TestAvaliaEscalaMilhar(t *testing.T) {
	f := Feeds{
		Ticker:      "ITSA4",
		NomeDoAtivo: "Itausa SA Non-Cum Perp Pfd Registered Shs", // brapi longName
		DenomCVM:    "ITAÚSA S.A.",
		AcoesEst:    144.923473130e9 / 13.04,
		AcoesTotal:  11213687,
		AcoesNet:    11213687 - 6337,
	}
	confirmado, diff, fator := Avalia(f)
	if !confirmado {
		t.Errorf("esperado confirmado, diff %v fator %v", diff, fator)
	}
	if fator != 1000 {
		t.Errorf("fator = %v, esperado 1000", fator)
	}
	if diff > 1 {
		t.Errorf("diff = %v, esperado ~0,9%%", diff)
	}
}

// Escala sozinha (sem nome) continua valendo apenas como sugestão.
func TestAvaliaEscalaSemNomeNaoConfirma(t *testing.T) {
	f := Feeds{
		Ticker:     "XYZ",
		AcoesEst:   11.1e9,
		AcoesTotal: 11213687,
		AcoesNet:   11213687 - 6337,
	}
	if confirmado, diff, _ := Avalia(f); confirmado || diff > ToleranciaPct {
		t.Errorf("numérica+escala sem nome não deve confirmar (diff %v)", diff)
	}
}

func TestNomesCompatVariedades(t *testing.T) {
	casos := []struct {
		a, b     string
		esperado bool
	}{
		{"PETROBRAS", "PETROLEO BRASILEIRO S.A. PETROBRAS", true},
		{"WEG SA", "WEG S.A.", true},
		{"Magazine Luiza S.A.", "MAGAZINE LUIZA SA", true},
		{"BCO DO BRASIL SA", "BCO DO BRASIL S.A.", true},
		{"AMBEV S.A.", "AMBEV S.A.", true},
		{"FUNDACAO A", "FUNDACAO B", false},
		{"", "PETROBRAS", false},
	}
	for _, c := range casos {
		if got := nomesCompat(c.a, c.b); got != c.esperado {
			t.Errorf("nomesCompat(%q, %q) = %v, esperado %v", c.a, c.b, got, c.esperado)
		}
	}
}
