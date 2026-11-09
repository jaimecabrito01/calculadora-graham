package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"calculadora-graham/internal/api"
	"calculadora-graham/internal/b3"
	"calculadora-graham/internal/brapi"
	"calculadora-graham/internal/config"
	"calculadora-graham/internal/cvm"
	"calculadora-graham/internal/etl"
	"calculadora-graham/internal/store"
)

func main() {
	if err := config.LoadDotenv(".env"); err != nil {
		log.Printf("aviso: falha ao ler .env: %v", err)
	}
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	if cfg.BrapiKey == "" {
		log.Println("aviso: BRAPI_API_KEY ausente — apenas PETR4, MGLU3, VALE3, ITUB4 respondem")
	}

	bg := context.Background()
	var db *store.Store
	if url := os.Getenv("DATABASE_URL"); url != "" {
		ctx, cancel := context.WithTimeout(bg, 10*time.Second)
		db, err = store.Open(ctx, url)
		cancel()
		if err != nil {
			log.Printf("aviso: PostgreSQL indisponível (%v) — modo CVM desativado", err)
			db = nil
		}
	} else {
		log.Println("aviso: DATABASE_URL ausente — fallback CVM desativado (só brapi)")
	}

	client := brapi.NewClient(cfg.BrapiURL, cfg.BrapiKey, cfg.ClientTO, cfg.MaxRetries)
	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           api.Routes(client, db, cfg.ServerTO),
		ReadHeaderTimeout: 5 * time.Second,
	}

	// sync sob demanda: no boot se nada recente no banco, depois semanal
	if db != nil {
		go func() {
			ctx, cancel := context.WithTimeout(bg, 30*time.Minute)
			defer cancel()
			ok, err := db.SincronizadoRecentemente(ctx)
			if err != nil {
				log.Printf("sync boot: consulta: %v", err)
				return
			}
			// ok = já há fundamentos recentes → só sincroniza no boot se faltar
			run(ctx, db, !ok)
			for range time.Tick(7 * 24 * time.Hour) {
				c, cancel := context.WithTimeout(bg, 30*time.Minute)
				run(c, db, true)
				cancel()
			}
		}()
	}

	go func() {
		log.Printf("calculadora-graham escutando em :%s", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("servidor: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	log.Println("encerrando...")
	ctx, cancel := context.WithTimeout(bg, 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("shutdown: %v", err)
	}
	if db != nil {
		db.Close()
	}
}

// run executa o ETL se necessário (no boot só se o banco está vazio; depois semanal).
func run(ctx context.Context, db *store.Store, precisa bool) {
	if !precisa {
		return
	}
	u := os.Getenv("BRAPI_URL")
	if u == "" {
		u = "https://brapi.dev/api"
	}
	s := etl.Novo(db, cvm.New(config.CVMCacheDir()), b3.New(), brapi.NewClient(u, os.Getenv("BRAPI_API_KEY"), 10*time.Second, 0))
	if err := s.Rodar(ctx); err != nil {
		log.Printf("sync: %v", err)
	}
}
