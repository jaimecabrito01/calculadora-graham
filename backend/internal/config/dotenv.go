package config

import (
	"bufio"
	"errors"
	"os"
	"strings"
)

// LoadDotenv aplica as linhas de KEY=value de um arquivo .env em os.Environ.
// Não sobrescreve variáveis já exportadas (env real tem prioridade).
// Sem dependência externa — arquivo ausente ou sem KEY=value é ignorado silenciosamente.
func LoadDotenv(paths ...string) error {
	for _, path := range paths {
		if path == "" {
			path = ".env"
		}
		f, err := os.Open(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return err
		}
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			key, value, ok := strings.Cut(line, "=")
			if !ok {
				continue
			}
			key = strings.TrimSpace(key)
			value = strings.TrimSpace(value)
			if key == "" {
				continue
			}
			if _, exists := os.LookupEnv(key); !exists {
				_ = os.Setenv(key, value)
			}
		}
		if err := scanner.Err(); err != nil {
			f.Close()
			return err
		}
		f.Close()
	}
	return nil
}
