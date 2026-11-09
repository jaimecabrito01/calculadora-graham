// Package mapping avalia evidências para ligar um ticker B3 a um CNPJ CVM:
//
//   - evidência nominal: nome do emissor (B3 COTAHIST NOMRES, ou longName da brapi)
//     contra DENOM_CIA (CVM), após normalização de sufixos legais e acentos;
//   - evidência numérica: marketCap ÷ preço (brapi) ≈ ações (CVM), com tolerância.
//
// O arquivo composicao_capital da CVM não traz coluna de escala e parte dos
// emissores reporta a contagem de ações em milhares (ex.: ITSA4, VALE3). Por isso
// a comparação numérica testa os fatores de escala conhecidos e devolve o fator
// aplicado, que deve ser persistido para corrigir o VPA.
//
// Regra do projeto (fixada pelo usuário): casamento numérico sozinho só sugere;
// confirmação automática exige evidência nominal + numérica.
package mapping

import (
	"math"
	"strings"

	"golang.org/x/text/unicode/norm"
)

// Tolerância do casamento numérico em percentual.
const ToleranciaPct = 10.0

// FatoresEscala cobre a contagem crua (1) e os emissores que reportam em milhares.
var FatoresEscala = []float64{1, 1000}

type Feeds struct {
	Ticker      string  // código B3
	NomeDoAtivo string  // nome visto pela brapi (/v2/tickers longName) e/ou B3 NOMRES
	DenomCVM    string  // DENOM_CIA
	AcoesEst    float64 // marketCap ÷ preço (0 se indisponível)
	AcoesTotal  float64 // CVM capital integralizado
	AcoesNet    float64 // CVM total − tesouraria (0 se desconhecido)
}

// Avalia devolve (confirmado, diff_pct, fator): diff é o melhor casamento numérico
// (0 quando não há evidência numérica) e fator é a escala que o produziu.
// Confirmado requer nome + numérica.
func Avalia(f Feeds) (confirmado bool, diffPct, fator float64) {
	nomeOK := nomesCompat(f.NomeDoAtivo, f.DenomCVM)
	diff, fator := MelhorFator(f.AcoesEst, f.AcoesTotal, f.AcoesNet)
	numOK := f.AcoesEst > 0 && diff <= ToleranciaPct
	return nomeOK && numOK, arredonda(diff), fator
}

// MelhorFator encontra o fator de escala que melhor aproxima AcoesEst das ações
// CVM (total ou líquida de tesouraria). diffPct = 0 e fator = 1 sem evidência.
func MelhorFator(acoesEst, acoesTotal, acoesNet float64) (diffPct, fator float64) {
	if acoesEst <= 0 {
		return 0, 1
	}
	best := math.MaxFloat64
	fator = 1
	for _, cand := range []float64{acoesTotal, acoesNet} {
		if cand <= 0 {
			continue
		}
		for _, fac := range FatoresEscala {
			ajust := cand * fac
			d := math.Abs((acoesEst - ajust) / ajust * 100)
			if d < best {
				best, fator = d, fac
			}
		}
	}
	if best == math.MaxFloat64 {
		return 0, 1
	}
	return best, fator
}

// nomesCompat compara (normalizados): igualdade, prefixo ou sufixo com >=4 caracteres
// (NOMRES é truncado e espelha o final do DENOM: "PETROBRAS" ⊂ "PETROLEO BRASILEIRO SA PETROBRAS").
func nomesCompat(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	na, nb := normalizaNome(a), normalizaNome(b)
	if na == nb {
		return true
	}
	if len(na) < 4 || len(nb) < 4 {
		return false
	}
	return strings.HasPrefix(na, nb) || strings.HasPrefix(nb, na) ||
		strings.HasSuffix(na, " "+nb) || strings.HasSuffix(nb, " "+na)
}

// normalizaNome remove acentos (NFD), pontuação e sufixos legais, e colapsa espaços.
func normalizaNome(s string) string {
	s = norm.NFD.String(strings.ToUpper(strings.TrimSpace(s)))
	var b strings.Builder
	for _, r := range s {
		if r >= 0x0300 && r <= 0x036f {
			continue // marca de acento da decomposição NFD
		}
		switch {
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ':
			b.WriteRune(' ')
		}
	}
	out := strings.Fields(b.String())
	// descarta sufixos legais no fim
	for len(out) > 0 {
		last := out[len(out)-1]
		if last == "SA" || last == "LTDA" || last == "CIA" || last == "COMPANY" || last == "PUBLICA" {
			out = out[:len(out)-1]
			continue
		}
		break
	}
	return strings.Join(out, " ")
}

func arredonda(v float64) float64 {
	return float64(int64(v*100+0.5)) / 100
}
