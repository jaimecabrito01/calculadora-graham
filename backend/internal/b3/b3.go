// Package b3 lê o arquivo diário COTAHIST da B3 para obter o nome do emissor
// (NOMRES) por código de negociação (CODNEG). O COTAHIST não traz CNPJ nem
// código CVM — ele serve como uma evidência cadastral para o mapeamento
// ticker→CNPJ, combinada com o nome da CVM e a validação numérica.
//
// Layout (SeriesHistoricas_Layout.pdf, registro 01, 245 bytes):
//
//	01-02 TIPREG="01"; 03-10 DATPRE (AAAAMMDD); 11-12 CODBDI ("02" lote padrão, "96" fracionário);
//	13-24 CODNEG; 25-27 TPMERC ("010" merc. a vista); 28-39 NOMRES.
//
// Download é best-effort: B3 pode estar inacessível/instável; qualquer falha
// retorna zip vazio sem fatalidade (o pipeline segue sem essa evidência).
package b3

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

const baseURL = "https://bvmf.bmfbovesp.com.br/InstDados/SerHist"

type Client struct {
	HTTP *http.Client
}

// Emissor é o resumo por ticker encontrado no arquivo.
type Emissor struct {
	Ticker string
	Nome   string // NOMRES, nome resumido do emissor
	Pregao string // data do pregão no formato AAAAMMDD
}

func New() *Client {
	return &Client{HTTP: &http.Client{Timeout: 45 * time.Second}}
}

// UltimosEmissores tenta os zips diários mais recentes (até 6 dias corridos atrás)
// e devolve o ticker→NOMRES do primeiro pregão alcançável. Grande parte dos
// ambientes tem firewall/proxy contra o host da B3; nesses casos devolve vazio.
func (c *Client) UltimosEmissores(ctx context.Context, agora time.Time) (map[string]Emissor, error) {
	var ultimoErro error
	for i := 0; i <= 6; i++ {
		dia := agora.AddDate(0, 0, -i)
		if dia.Weekday() == time.Sunday || dia.Weekday() == time.Saturday {
			continue
		}
		em, err := c.dia(ctx, dia)
		if err == nil {
			return em, nil
		}
		ultimoErro = err
	}
	if ultimoErro != nil {
		log.Printf("b3: indisponível (%v) — seguindo sem evidência cadastral B3", ultimoErro)
	}
	return map[string]Emissor{}, nil
}

func (c *Client) dia(ctx context.Context, d time.Time) (map[string]Emissor, error) {
	nome := fmt.Sprintf("COTAHIST_D%s.zip", d.Format("020106"))
	url := fmt.Sprintf("%s/%s", baseURL, nome)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: status %d", nome, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return nil, err
	}
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return nil, err
	}
	for _, zf := range zr.File {
		if !strings.HasSuffix(zf.Name, ".TXT") {
			continue
		}
		rc, err := zf.Open()
		if err != nil {
			return nil, err
		}
		defer rc.Close()
		return parseCotahist(rc)
	}
	return nil, fmt.Errorf("%s: zip sem TXT", nome)
}

// parseCotahist extrai registros tipo 01 do mercado a vista (CODBDI 02 ou 96),
// mantendo por ticker o nome má. resumido visto.
func parseCotahist(r io.Reader) (map[string]Emissor, error) {
	out := map[string]Emissor{}
	buf := new(bytes.Buffer)
	if _, err := io.Copy(buf, io.LimitReader(r, 64<<20)); err != nil {
		return nil, err
	}
	for linha := range strings.SplitSeq(buf.String(), "\n") {
		linha = strings.TrimRight(linha, "\r")
		if len(linha) < 40 {
			continue
		}
		if linha[0:2] != "01" {
			continue
		}
		tpmerc := linha[24:27]
		if tpmerc != "010" && tpmerc != "020" {
			continue
		}
		bdi := strings.TrimSpace(linha[10:12])
		if bdi != "02" && bdi != "96" {
			continue
		}
		ticker := strings.TrimSpace(linha[12:24])
		nome := strings.TrimSpace(linha[27:39])
		if ticker == "" || nome == "" {
			continue
		}
		pregao := linha[2:10]
		ant, ok := out[ticker]
		if !ok {
			out[ticker] = Emissor{Ticker: ticker, Nome: nome, Pregao: pregao}
		} else if pregao > ant.Pregao {
			out[ticker] = Emissor{Ticker: ticker, Nome: nome, Pregao: pregao}
		}
	}
	return out, nil
}
