package config

import (
	"errors"
	"os"
	"strconv"
	"time"
)

type Config struct {
	Port        string
	BrapiKey    string
	BrapiURL    string
	ServerTO    time.Duration
	ClientTO    time.Duration
	MaxRetries  int
	FreeTickers map[string]struct{}
}

// CVMCacheDir devolve o diretório de cache dos zips da CVM (CVM_CACHE_DIR,
// default "data" relativo ao cwd). No container o compose aponta para /app/data.
func CVMCacheDir() string {
	if d := os.Getenv("CVM_CACHE_DIR"); d != "" {
		return d
	}
	return "data"
}

func Load() (Config, error) {
	cfg := Config{
		Port:        "8080",
		BrapiKey:    os.Getenv("BRAPI_API_KEY"),
		BrapiURL:    "https://brapi.dev/api",
		ServerTO:    10 * time.Second,
		ClientTO:    5 * time.Second,
		MaxRetries:  1,
		FreeTickers: map[string]struct{}{"PETR4": {}, "MGLU3": {}, "VALE3": {}, "ITUB4": {}},
	}
	if p := os.Getenv("PORT"); p != "" {
		if _, err := strconv.Atoi(p); err != nil {
			return cfg, errors.New("PORT inválida: " + p)
		}
		cfg.Port = p
	}
	if u := os.Getenv("BRAPI_URL"); u != "" {
		cfg.BrapiURL = u
	}
	return cfg, nil
}
