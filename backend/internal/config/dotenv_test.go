package config

import (
	"os"
	"path/filepath"
	"testing"
)

func tsetenv(t *testing.T, key, val string) {
	t.Helper()
	t.Cleanup(func() { _ = os.Unsetenv(key) })
	_ = os.Setenv(key, val)
}

func TestLoadDotenv(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "test.env")
	content := "# comentário\nKEY_BARE=123\n\nVAZIO=\nMERGE=x\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	tsetenv(t, "MERGE", "prioridade-env")

	if err := LoadDotenv(path); err != nil {
		t.Fatalf("LoadDotenv: %v", err)
	}
	if v, _ := os.LookupEnv("KEY_BARE"); v != "123" {
		t.Errorf("KEY_BARE = %q, esperado 123", v)
	}
	if v, _ := os.LookupEnv("VAZIO"); v != "" {
		t.Errorf("VAZIO = %q, esperado vazio", v)
	}
	if v, _ := os.LookupEnv("MERGE"); v != "prioridade-env" {
		t.Errorf("MERGE = %q, esperado prioridade-env (env exportada não é sobrescrita)", v)
	}
	// arquivo ausente não é erro
	if err := LoadDotenv(filepath.Join(tmp, "nao-existe.env")); err != nil {
		t.Errorf("arquivo ausente: esperado nil, veio %v", err)
	}
}

func TestLoadDotenvArquivoInvalido(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "dir.env")
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := LoadDotenv(path); err == nil {
		t.Errorf("esperado erro ao abrir diretório como .env")
	}
}

func TestCVMCacheDir(t *testing.T) {
	t.Setenv("CVM_CACHE_DIR", "")
	if got := CVMCacheDir(); got != "data" {
		t.Errorf("default = %q, esperado data", got)
	}
	t.Setenv("CVM_CACHE_DIR", "/app/data")
	if got := CVMCacheDir(); got != "/app/data" {
		t.Errorf("env = %q, esperado /app/data", got)
	}
}
