package api

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"calculadora-graham/internal/brapi"
)

// cors liberado para dev do front; restringir por origem quando em produção.
func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func Routes(b *brapi.Client, db FonteCVM, serverTimeout time.Duration) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(cors)

	r.Get("/healthz", Healthz)

	s := &Server{brapi: b, db: db}
	r.Route("/api", func(r chi.Router) {
		r.Use(middleware.Timeout(serverTimeout))
		r.Get("/ativos", s.Ativos)
		r.Get("/graham/{ticker}", s.Graham)
	})
	return r
}
