package cvm

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/text/encoding/charmap"
)

// criaZipLatino grava um zip com CSVs codificados em latin-1 (como os reais da CVM).
func criaZipLatino(t *testing.T, dir string, arquivos map[string]string) string {
	t.Helper()
	path := filepath.Join(dir, "fonte_fixa.zip")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("criar zip: %v", err)
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	for nome, conteudo := range arquivos {
		enc, err := charmap.ISO8859_1.NewEncoder().Bytes([]byte(conteudo))
		if err != nil {
			t.Fatalf("encoding: %v", err)
		}
		w, err := zw.Create(nome)
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		if _, err := w.Write(enc); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	return path
}

func TestProcessaZipBasico(t *testing.T) {
	dir := t.TempDir()
	bpp := "CNPJ_CIA;DT_REFER;VERSAO;DENOM_CIA;CD_CVM;GRUPO_DFP;MOEDA;ESCALA_MOEDA;ORDEM_EXERC;DT_FIM_EXERC;CD_CONTA;DS_CONTA;VL_CONTA;ST_CONTA_FIXA\n" +
		// PENÚLTIMO (deve ser ignorado por ORDEM)
		"33.000.167/0001-01;2024-12-31;1;PETROBRAS;009512;DFP - BPP;BRL;MIL;PENÚLTIMO;2024-06-30;2.03;PL Cons.;367514000.0000000000;S\n" +
		// ÚLTIMO
		"33.000.167/0001-01;2024-12-31;1;PETROBRAS;009512;DFP - BPP;BRL;MIL;ÚLTIMO;2024-12-31;2.03;PL Cons.;367514000.0000000000;S\n" +
		// conta irmã (2.03.01) não deve vazar
		"33.000.167/0001-01;2024-12-31;1;PETROBRAS;009512;DFP - BPP;BRL;MIL;ÚLTIMO;2024-12-31;2.03.01;Capital;205432000.0000000000;S\n" +
		// outra empresa: versão mais nova do mesmo período vence
		"07.275.159/0001-68;2024-12-31;2;LUBNORITE;001234;DFP - BPP;BRL;MIL;ÚLTIMO;2024-12-31;2.03;PL;100000.0000000000;S\n" +
		"07.275.159/0001-68;2024-12-31;1;LUBNORITE;001234;DFP - BPP;BRL;MIL;ÚLTIMO;2024-12-31;2.03;PL;95000.0000000000;S\n"
	cap := "CNPJ_CIA;DT_REFER;VERSAO;DENOM_CIA;QT_ACAO_ORDIN_CAP_INTEGR;QT_ACAO_PREF_CAP_INTEGR;QT_ACAO_TOTAL_CAP_INTEGR;QT_ACAO_ORDIN_TESOURO;QT_ACAO_PREF_TESOURO;QT_ACAO_TOTAL_TESOURO\n" +
		"33.000.167/0001-01;2024-12-31;1;PETROBRAS;7442454142;5602042788;13044496930;222760;155541409;155764169\n" +
		"07.275.159/0001-68;2024-12-31;2;LUBNORITE;1000;2000;3000;0;0;0\n" +
		// sem ações não deve alimentar
		"09.999.999/0001-99;2024-12-31;1;FANTASMA;0;0;0;0;0;0\n"
	path := criaZipLatino(t, dir, map[string]string{
		"dfp_cia_aberta_composicao_capital_2024.csv": cap,
		"dfp_cia_aberta_BPP_con_2024.csv":            bpp,
	})

	c := New(dir)
	regs, err := c.processaZip(path)
	if err != nil {
		t.Fatalf("processaZip: %v", err)
	}
	if len(regs) != 2 {
		t.Fatalf("esperado 2 registros, veio %d: %v", len(regs), regs)
	}
	porCNPJ := map[string]Fundamentos{}
	for _, r := range regs {
		porCNPJ[r.CNPJ] = r
	}
	p := porCNPJ["33000167000101"]
	if p.PL != 367514000000 {
		t.Errorf("PETRO PL = %v, esperado 367514000000 (MIL ×1000)", p.PL)
	}
	if p.AcoesTotal != 13044496930 || p.AcoesTesouraria != 155764169 {
		t.Errorf("PETRO ações = %v/%v", p.AcoesTotal, p.AcoesTesouraria)
	}
	if p.DataFimExerc != "2024-12-31" {
		t.Errorf("dtFim = %q", p.DataFimExerc)
	}
	l := porCNPJ["07275159000168"]
	if l.PL != 100000*1000 { // versão 2 vence a 1
		t.Errorf("LUB PL = %v, esperado versão 2", l.PL)
	}
	if _, existe := porCNPJ["09999999000199"]; existe {
		t.Errorf("FANTASMA não deveria existir")
	}
}

func TestPegaDTReferMaisRecente(t *testing.T) {
	dir := t.TempDir()
	bpp := "CNPJ_CIA;DT_REFER;VERSAO;ORDEM_EXERC;ESCALA_MOEDA;DT_FIM_EXERC;CD_CONTA;VL_CONTA\n" +
		"11111111000111;2024-12-31;1;ÚLTIMO;MIL;2024-12-31;2.03;100000\n" +
		"11111111000111;2023-12-31;5;ÚLTIMO;MIL;2023-12-31;2.03;900000\n"
	cap := "CNPJ_CIA;DT_REFER;VERSAO;QT_ACAO_TOTAL_CAP_INTEGR;QT_ACAO_TOTAL_TESOURO\n" +
		"11111111000111;2024-12-31;1;2000;0\n" +
		"11111111000111;2023-12-31;5;1800;0\n"
	path := criaZipLatino(t, dir, map[string]string{
		"itr_cia_aberta_composicao_capital_2026.csv": cap,
		"itr_cia_aberta_BPP_con_2026.csv":            bpp,
	})
	c := New(dir)
	regs, err := c.processaZip(path)
	if err != nil {
		t.Fatalf("processaZip: %v", err)
	}
	if len(regs) != 1 || regs[0].PL != 100000*1000 || regs[0].AcoesTotal != 2000 {
		t.Fatalf("esperado registro 2024 (dt_ref mais recente): %v", regs)
	}
}

func TestNormalizaCNPJ(t *testing.T) {
	if got := normalizaCNPJ("33.000.167/0001-01"); got != "33000167000101" {
		t.Errorf("normalizaCNPJ = %q", got)
	}
}

// TestProcessaZipComReais é manual: baixe os zips reais para data/ e remova o skip.
func TestProcessaZipComReais(t *testing.T) {
	t.Skip("manual: use data/ com zips CVM reais")
	dir := "data"
	for _, nome := range []string{"dfp_cia_aberta_2025.zip", "itr_cia_aberta_2026.zip"} {
		if _, err := os.Stat(filepath.Join(dir, nome)); err != nil {
			continue
		}
		c := New(dir)
		regs, err := c.processaZip(filepath.Join(dir, nome))
		if err != nil {
			t.Errorf("%s: %v", nome, err)
			continue
		}
		for _, r := range regs {
			if strings.HasPrefix(r.CNPJ, "330001") {
				t.Logf("%s PETRO: PL=%.0f ações=%.0f/%.0f", nome, r.PL, r.AcoesTotal, r.AcoesTesouraria)
			}
		}
	}
}
