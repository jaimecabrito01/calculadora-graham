// Package cvm baixa e processa as demonstrações da CVM (DFP e ITR)
// para obter Patrimônio Líquido consolidado (conta 2.03, controlador puro)
// e quantidade de ações (composicao_capital), por CNPJ.
//
// Layout verificado em 2026-11 (detalhes no AGENTS.md):
//   - zips por ano: dados.cvm.gov.br/dados/CIA_ABERTA/DOC/{DFP,ITR}/DADOS/
//   - CSVs com separador ";" e encoding latin-1
//   - BPP_con: CD_CONTA "2.03" = PL Consolidado; ESCALA_MOEDA MIL; ORDEM_EXERC "ÚLTIMO"
//   - composicao_capital: QT_ACAO_TOTAL_CAP_INTEGR e QT_ACAO_TOTAL_TESOURO
package cvm

import (
	"archive/zip"
	"bufio"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/text/encoding/charmap"
)

const (
	baseURLDvm  = "https://dados.cvm.gov.br/dados"
	cacheTTL    = 7 * 24 * time.Hour
	maxZipBytes = 256 << 20 // zip até 256MB (DFP ~13MB, DMPL grandes dentro; folga)
)

// Fundamentos é o registro consolidado de uma empresa CVM.
type Fundamentos struct {
	CNPJ            string  // apenas dígitos
	Fonte           string  // "dfp" | "itr" (define Sincroniza a partir do zip)
	Denom           string  // DENOM_CIA (evidência nominal do mapeamento)
	DataReferencia  string  // DT_REFER (YYYY-MM-DD)
	DataFimExerc    string  // DT_FIM_EXERC
	PL              float64 // conta 2.03, já em reais
	AcoesTotal      float64 // capital social integralizado (todas as classes)
	AcoesTesouraria float64 // ações em tesouraria
}

// Client baixa (com cache local) e processa os zips da CVM.
type Client struct {
	HTTP *http.Client
	Dir  string // diretório de cache (default "data")
}

func New(cacheDir string) *Client {
	if cacheDir == "" {
		cacheDir = "data"
	}
	return &Client{HTTP: &http.Client{Timeout: 4 * time.Minute}, Dir: cacheDir}
}

var errIndisponivel = errors.New("zip não disponível no portal")

// Sincroniza baixa e processa DFP (ano atual + anterior) e ITR (ano atual).
// Zip indisponível é tratado como não fatal (registrado e ignorado).
func (c *Client) Sincroniza(ctx context.Context, agora time.Time) ([]Fundamentos, []error) {
	fontes := []struct{ doc, ano string }{
		{"DFP", strconv.Itoa(agora.Year() - 1)},
		{"DFP", strconv.Itoa(agora.Year())},
		{"ITR", strconv.Itoa(agora.Year())},
	}
	var out []Fundamentos
	var erros []error
	for _, f := range fontes {
		doc := strings.ToLower(f.doc)
		nomeZip := fmt.Sprintf("%s_cia_aberta_%s.zip", doc, f.ano)
		url := fmt.Sprintf("%s/CIA_ABERTA/DOC/%s/DADOS/%s", baseURLDvm, f.doc, nomeZip)
		if err := c.baixaComCache(ctx, url, nomeZip); err != nil {
			if errors.Is(err, errIndisponivel) {
				erros = append(erros, fmt.Errorf("%s: %w", nomeZip, err))
				continue
			}
			erros = append(erros, fmt.Errorf("%s: download: %w", nomeZip, err))
			continue
		}
		regs, err := c.processaZip(filepath.Join(c.Dir, nomeZip))
		if err != nil {
			erros = append(erros, fmt.Errorf("%s: %w", nomeZip, err))
			continue
		}
		for i := range regs {
			regs[i].Fonte = doc // "dfp" | "itr"
		}
		log.Printf("cvm: %s processado (%d CNPJs)", nomeZip, len(regs))
		out = append(out, regs...)
	}
	return out, erros
}

// baixaComCache mantém o zip sob Dir por cacheTTL; renomeia .part ao concluir.
func (c *Client) baixaComCache(ctx context.Context, url, nomeZip string) error {
	caminho := filepath.Join(c.Dir, nomeZip)
	if st, err := os.Stat(caminho); err == nil && time.Since(st.ModTime()) < cacheTTL {
		return nil
	}
	if err := os.MkdirAll(c.Dir, 0o755); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("request: %w", err)
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound, http.StatusForbidden:
		return errIndisponivel
	default:
		return fmt.Errorf("status %d", resp.StatusCode)
	}
	tmp := caminho + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	n, err := io.Copy(f, io.LimitReader(resp.Body, maxZipBytes))
	if err != nil {
		f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if n == 0 {
		_ = os.Remove(tmp)
		return errors.New("arquivo vazio")
	}
	return os.Rename(tmp, caminho)
}

// processaZip extrai PL (BPP_con, conta 2.03) e composição de capital de um zip
// e devolve os registros onde os dois lados convergem por CNPJ.
func (c *Client) processaZip(zipPath string) ([]Fundamentos, error) {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return nil, err
	}
	defer zr.Close()

	pl := map[string]bppRegistro{}
	acoes := map[string]capRegistro{}
	for _, zf := range zr.Reader.File {
		switch {
		case strings.Contains(zf.Name, "_BPP_con_"):
			if err := leBPPCon(zf, pl); err != nil {
				return nil, fmt.Errorf("BPP_con (%s): %w", zf.Name, err)
			}
		case strings.Contains(zf.Name, "composicao_capital"):
			if err := leComposicaoCapital(zf, acoes); err != nil {
				return nil, fmt.Errorf("composicao_capital (%s): %w", zf.Name, err)
			}
		}
	}
	out := []Fundamentos{}
	for cnpj, capReg := range acoes {
		plReg, ok := pl[cnpj]
		if !ok {
			continue
		}
		out = append(out, Fundamentos{
			CNPJ:            cnpj,
			Denom:           capReg.denom,
			DataReferencia:  capReg.dtRef,
			DataFimExerc:    plReg.dtFim,
			PL:              plReg.valor,
			AcoesTotal:      capReg.total,
			AcoesTesouraria: capReg.tes,
		})
	}
	return out, nil
}

// bppRegistro é a linha 2.03 escolhida de um CNPJ.
type bppRegistro struct {
	dtRef string
	dtFim string
	valor float64
	ver   int
}

// capRegistro é a linha de composição de capital escolhida de um CNPJ.
type capRegistro struct {
	dtRef string
	total float64
	tes   float64
	ver   int
	denom string
}

// leBPPCon mantém, por CNPJ, o registro com maior DT_REFER (empate: maior VERSAO).
func leBPPCon(z *zip.File, out map[string]bppRegistro) error {
	csvR, closeFn, err := abrirCSV(z)
	if err != nil {
		return err
	}
	defer closeFn()

	cab, err := csvR.Read()
	if err != nil {
		return fmt.Errorf("header: %w", err)
	}
	col, err := mapeiaColunas(cab, []string{"cnpj", "dtRef", "ver", "ordem", "escala", "conta", "fim", "valor"})

	if err != nil {
		return err
	}

	for {
		linha, err := csvR.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("linha: %w", err)
		}
		if col(linha, "conta") != "2.03" {
			continue
		}
		if col(linha, "ordem") != "ÚLTIMO" {
			continue
		}
		valor, _, err := parseValor(col(linha, "valor"), col(linha, "escala"))
		if err != nil {
			continue // linha malformada não aborta o processamento
		}
		cnpj := normalizaCNPJ(col(linha, "cnpj"))
		dtRef := col(linha, "dtRef")
		ver, _ := strconv.Atoi(col(linha, "ver"))

		reg := bppRegistro{dtRef: dtRef, dtFim: col(linha, "fim"), valor: valor, ver: ver}
		ant, ok := out[cnpj]
		if !ok || melhora(reg.dtRef, reg.ver, ant.dtRef, ant.ver) {
			out[cnpj] = reg
		}
	}
	return nil
}

// leComposicaoCapital mantém, por CNPJ, o registro com maior DT_REFER (empate: maior VERSAO).
func leComposicaoCapital(z *zip.File, out map[string]capRegistro) error {
	csvR, closeFn, err := abrirCSV(z)
	if err != nil {
		return err
	}
	defer closeFn()

	cab, err := csvR.Read()
	if err != nil {
		return fmt.Errorf("header: %w", err)
	}
	col, err := mapeiaColunas(cab, []string{"cnpj", "dtRef", "ver", "total", "tes"})
	if err != nil {
		return err
	}
	denomOpc, _ := mapeiaColunas(cab, []string{"denom"}) // DENOM_CIA pode variar; é opcional
	for {
		linha, err := csvR.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("linha: %w", err)
		}
		total := numero(col(linha, "total"))
		tes := numero(col(linha, "tes"))
		if total == 0 && tes == 0 {
			continue
		}
		cnpj := normalizaCNPJ(col(linha, "cnpj"))
		dtRef := col(linha, "dtRef")
		ver, _ := strconv.Atoi(col(linha, "ver"))

		reg := capRegistro{dtRef: dtRef, total: total, tes: tes, ver: ver, denom: denomOpc(linha, "denom")}
		ant, ok := out[cnpj]
		if !ok || melhora(reg.dtRef, reg.ver, ant.dtRef, ant.ver) {
			out[cnpj] = reg
		}
	}
	return nil
}

// melhora compara candidata (dt, versão) com a atual: data maior vence; empate vira versão maior.
func melhora(candDT string, candVer int, atualDTOld string, atualVer int) bool {
	if candDT != atualDTOld {
		return candDT > atualDTOld
	}
	return candVer > atualVer
}

// mapeiaColunas devolve um acessor por nome lógico, tolerando variações de header
// (DFP usa CNPJ_CIA/DT_REFER/VERSAO; ITR usa CNPJ_Companhia/Data_Referencia/Versao).
func mapeiaColunas(cab []string, nomes []string) (func([]string, string) string, error) {
	variacao := map[string][]string{
		"cnpj":   {"CNPJ_CIA", "CNPJ_Companhia"},
		"dtRef":  {"DT_REFER", "Data_Referencia"},
		"ver":    {"VERSAO", "Versao"},
		"ordem":  {"ORDEM_EXERC"},
		"escala": {"ESCALA_MOEDA"},
		"conta":  {"CD_CONTA"},
		"fim":    {"DT_FIM_EXERC"},
		"valor":  {"VL_CONTA"},
		"total":  {"QT_ACAO_TOTAL_CAP_INTEGR"},
		"tes":    {"QT_ACAO_TOTAL_TESOURO"},
		"denom":  {"DENOM_CIA", "Denom_Cia"},
	}
	idx := map[string]int{}
	for _, nome := range nomes {
		for _, variante := range variacao[nome] {
			for i, h := range cab {
				if strings.EqualFold(strings.TrimSpace(h), variante) {
					idx[nome] = i
				}
			}
		}
	}
	for _, nome := range nomes {
		if _, ok := idx[nome]; !ok {
			// mesmo com coluna ausente devolve um acessor seguro (uso opcional);
			// o required falha como erro para o chamador decidir
			return func(linha []string, _ string) string { return "" },
				fmt.Errorf("coluna %s ausente no header %q", nome, strings.Join(cab, ";"))
		}
	}
	return func(linha []string, nome string) string {
		i, ok := idx[nome]
		if !ok || i >= len(linha) {
			return ""
		}
		return strings.TrimSpace(linha[i])
	}, nil
}

// parseValor interpreta VL_CONTA aplicando a escala (MIL → ×1000).
func parseValor(raw, escala string) (float64, string, error) {
	v, err := strconv.ParseFloat(strings.TrimSpace(strings.Replace(raw, ",", ".", 1)), 64)
	if err != nil {
		return 0, escala, err
	}
	switch escala {
	case "MIL":
		return v * 1000, escala, nil
	case "UNIDADE", "":
		return v, "UNIDADE", nil
	default:
		return 0, escala, fmt.Errorf("escala não suportada: %s", escala)
	}
}

// numero parseia quantidade inteira planejada ("13044496930").
func numero(raw string) float64 {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0
	}
	return v
}

// NormalizaCNPJ reduz "33.000.167/0001-01" a "33000167000101" (exportada
// para o sync validar/promover CNPJs no mesmo formato normalizado do parser).
func NormalizaCNPJ(cnpj string) string {
	var b strings.Builder
	for _, r := range cnpj {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// normalizaCNPJ é o apelido interno usado pelo parser.
func normalizaCNPJ(cnpj string) string { return NormalizaCNPJ(cnpj) }

// abrirCSV decodifica latin-1 → UTF-8 e devolve reader CSV pronto para leitura.
func abrirCSV(z *zip.File) (*csv.Reader, func(), error) {
	rc, err := z.Open()
	if err != nil {
		return nil, nil, err
	}
	dec := charmap.ISO8859_1.NewDecoder().Reader(rc)
	br := bufio.NewReader(dec)
	r := csv.NewReader(br)
	r.Comma = ';'
	r.LazyQuotes = true
	r.FieldsPerRecord = -1
	return r, func() { rc.Close() }, nil
}
