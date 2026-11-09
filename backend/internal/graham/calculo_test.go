package graham

import (
	"errors"
	"math"
	"testing"
)

func f(v float64) *float64 { return &v }

func TestCalcularPETR4(t *testing.T) {
	// dados reais brapi 2026-11-06
	res, err := Calcular("PETR4", "Petroleo Brasileiro SA Pfd", f(45.76), f(10.35), f(37.32))
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	esperado := math.Sqrt(22.5 * 10.35 * 37.32)
	if math.Abs(*res.PrecoJusto-esperado) > 1e-9 {
		t.Errorf("preco_justo = %v, esperado %v", *res.PrecoJusto, esperado)
	}
	wantMargem := (esperado - 45.76) / 45.76 * 100
	if math.Abs(*res.Margem-wantMargem) > 1e-9 {
		t.Errorf("margem = %v, esperado %v", *res.Margem, wantMargem)
	}
	if res.Classificacao != "descontada" {
		t.Errorf("classificacao = %q, esperado descontada", res.Classificacao)
	}
}

func TestCalcularSobreavaliada(t *testing.T) {
	res, err := Calcular("X", "X SA", f(100), f(1), f(1))
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	esperado := math.Sqrt(22.5) // LPA=1, VPA=1
	if math.Abs(*res.PrecoJusto-esperado) > 1e-9 {
		t.Errorf("preco_justo = %v, esperado %v", *res.PrecoJusto, esperado)
	}
	if res.Classificacao != "sobreavaliada" {
		t.Errorf("classificacao = %q", res.Classificacao)
	}
}

func TestCalcularSemPreco(t *testing.T) {
	res, err := Calcular("X", "X SA", nil, f(2), f(4))
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if res.PrecoAtual != nil || res.Margem != nil {
		t.Errorf("preco/margem deveriam ser null")
	}
	if res.Classificacao != "" {
		t.Errorf("classificacao deveria ser vazia")
	}
}

func TestCalcularLPAInvalido(t *testing.T) {
	casos := []struct {
		nome string
		lpa  *float64
		vpa  *float64
	}{
		{"lpa nulo", nil, f(1)},
		{"lpa negativo", f(-1), f(1)},
		{"vpa nulo", f(1), nil},
		{"vpa negativo", f(1), f(-1)},
	}
	for _, c := range casos {
		_, err := Calcular("X", "X SA", f(10), c.lpa, c.vpa)
		var inap *Inaplicavel
		if !errors.As(err, &inap) {
			t.Errorf("%s: esperado Inaplicavel, veio %v", c.nome, err)
		}
	}
}

func TestResultadoFixtureMGLU3(t *testing.T) {
	// MGLU3 real 2026-11-06: LPA 0,1144 / VPA 14,39 / preço 5,66
	res, err := Calcular("MGLU3", "Magazine Luiza S.A.", f(5.66), f(0.1144), f(14.39))
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	esperado := math.Sqrt(22.5 * 0.1144 * 14.39)
	if math.Abs(*res.PrecoJusto-esperado) > 1e-9 {
		t.Errorf("preco_justo = %v, esperado %v", *res.PrecoJusto, esperado)
	}
	if *res.PrecoJusto <= 5.66 || res.Classificacao != "descontada" {
		t.Errorf("preco_justo = %v / classificacao = %q", *res.PrecoJusto, res.Classificacao)
	}
}
