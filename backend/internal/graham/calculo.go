package graham

import "math"

const ConstanteLiq = 22.5

// Calculo é o resultado completo para um ativo, pronto para o front.
type Calculo struct {
	Ticker        string   `json:"ticker"`
	Nome          string   `json:"nome"`
	Logo          string   `json:"logo,omitempty"`           // ícone do ativo (brapi)
	PrecoAtual    *float64 `json:"preco_atual"`              // R$
	LPA           *float64 `json:"lpa"`                      // lucro por ação
	VPA           *float64 `json:"vpa"`                      // valor patrimonial por ação
	PrecoJusto    *float64 `json:"preco_justo"`              // V = √(22,5 × LPA × VPA)
	Margem        *float64 `json:"margem"`                   // %: (justo − atual)/atual × 100
	Classificacao string   `json:"classificacao"`            // "descontada" | "sobreavaliada"
	VpaFonte      string   `json:"vpa_fonte,omitempty"`      // "brapi" | "cvm"
	VpaReferencia string   `json:"vpa_referencia,omitempty"` // data do balanço que gerou o VPA
}

// Inaplicavel reporta por que Graham não se aplica ao ativo.
type Inaplicavel struct {
	Razao string
}

func (e *Inaplicavel) Error() string { return e.Razao }

// Calcular aplica V = √(22,5 × LPA × VPA) e deriva margem/classificação
// comparando com o preço atual. A brapi retorna LPA em Price/EPS e VPA em
// defaultKeyStatistics.bookValue; se algum vier nulo ou não positivo,
// Graham é inaplicável.
func Calcular(ticker, nome string, preco, lpa, vpa *float64) (Calculo, error) {
	if lpa == nil || *lpa <= 0 {
		return Calculo{}, &Inaplicavel{Razao: "Graham inaplicável: LPA ausente ou negativo para este ativo"}
	}
	if vpa == nil || *vpa <= 0 {
		return Calculo{}, &Inaplicavel{Razao: "Graham inaplicável: VPA ausente ou negativo para este ativo"}
	}
	justo := math.Sqrt(ConstanteLiq * *lpa * *vpa)
	res := Calculo{
		Ticker:     ticker,
		Nome:       nome,
		LPA:        lpa,
		VPA:        vpa,
		PrecoJusto: &justo,
	}
	if preco != nil && *preco > 0 {
		res.PrecoAtual = preco
		margem := (justo - *preco) / *preco * 100
		res.Margem = &margem
		if margem >= 0 {
			res.Classificacao = "descontada"
		} else {
			res.Classificacao = "sobreavaliada"
		}
	}
	return res, nil
}
