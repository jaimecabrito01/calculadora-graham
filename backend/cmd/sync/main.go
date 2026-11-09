package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"calculadora-graham/internal/b3"
	"calculadora-graham/internal/brapi"
	"calculadora-graham/internal/config"
	"calculadora-graham/internal/cvm"
	"calculadora-graham/internal/etl"
	"calculadora-graham/internal/store"
)

func main() {
	var promotes []string
	var listar, semB3, semSugestoes, semCVM bool
	flag.Func("promote", "confirma mapeamento manual: -promote TICKER=CNPJ (repita para vários)", func(v string) error {
		if _, _, ok := strings.Cut(v, "="); !ok {
			return fmt.Errorf("formato TICKER=CNPJ: %q", v)
		}
		cnpj := v[strings.LastIndex(v, "=")+1:]
		if len(cvm.NormalizaCNPJ(cnpj)) != 14 {
			return fmt.Errorf("CNPJ inválido: %q", cnpj)
		}
		promotes = append(promotes, v)
		return nil
	})
	flag.BoolVar(&listar, "listar", false, "lista mapeamentos confirmados e sugestões pendentes")
	flag.BoolVar(&semB3, "sem-b3", false, "pula a tentativa B3 (proxy restrito)")
	flag.BoolVar(&semSugestoes, "sem-sugestoes", false, "não gera sugestões numéricas (só sync CVM)")
	flag.BoolVar(&semCVM, "sem-cvm", false, "pula o sync CVM (útil para só promote/listar)")
	flag.Parse()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	db, err := store.OpenFromEnv(ctx)
	if err != nil {
		log.Fatalf("store: %v", err)
	}
	defer db.Close()

	// overrides manuais valem sempre; executados antes do resto
	br := brapiClient()
	for _, p := range promotes {
		ticker, cnpj, _ := strings.Cut(p, "=")
		t, c := strings.ToUpper(strings.TrimSpace(ticker)), cvm.NormalizaCNPJ(cnpj)
		if err := db.UpsertMappingConfirmado(ctx, store.Mapping{Ticker: t, CNPJ: c, Fonte: "manual"}); err != nil {
			log.Fatalf("promote %s: %v", p, err)
		}
		log.Printf("promote: %s → %s confirmado", t, c)
		// calibra a escala das ações (CVM não informa unidade); best effort
		if fator, err := etl.CalibraFator(ctx, db, br, t, c); err != nil {
			log.Printf("promote: aviso calibração de escala: %v (VPA pode ficar incorreto; rode o sync)", err)
		} else if fator != 1 {
			log.Printf("promote: escala de ações de %s corrigida (fator %g)", t, fator)
		}
	}

	if listar {
		if err := listarTudo(ctx, db); err != nil {
			log.Fatalf("listar: %v", err)
		}
		return
	}

	if !semCVM {
		s := etl.Novo(db, cvm.New(config.CVMCacheDir()), b3.New(), br)
		s.SemB3(semB3)
		s.SemSugestoes(semSugestoes)
		runCtx, runCancel := context.WithTimeout(ctx, 25*time.Minute)
		defer runCancel()
		if err := s.Rodar(runCtx); err != nil {
			log.Fatalf("sync: %v", err)
		}
	}

	if err := listarTudo(ctx, db); err != nil {
		log.Fatalf("listar final: %v", err)
	}
}

func brapiClient() *brapi.Client {
	u := os.Getenv("BRAPI_URL")
	if u == "" {
		u = "https://brapi.dev/api"
	}
	return brapi.NewClient(u, os.Getenv("BRAPI_API_KEY"), 10*time.Second, 0)
}

func listarTudo(ctx context.Context, db *store.Store) error {
	confirmados, err := db.MapeamentosConfirmados(ctx)
	if err != nil {
		return err
	}
	fmt.Printf("=== Mapeamentos confirmados (%d) ===\n", len(confirmados))
	for _, m := range confirmados {
		fmt.Printf("  %-10s → %s [%s]\n", m.Ticker, m.CNPJ, m.Fonte)
	}
	sug, err := db.ListaSugestoes(ctx)
	if err != nil {
		return err
	}
	fmt.Printf("=== Sugestões numéricas pendentes (%d) — promova manualmente com -promote TICKER=CNPJ ===\n", len(sug))
	for _, s := range sug {
		fmt.Printf("  %-10s → %s (diff %.2f%%)\n", s.Ticker, s.CNPJ, s.DiffPct)
	}
	return nil
}
