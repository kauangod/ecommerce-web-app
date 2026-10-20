package execucao

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCarregarEnvPreservaVariaveisDoTerminal(t *testing.T) {
	caminho := filepath.Join(t.TempDir(), ".env")
	const existente = "ECOMMERCE_TESTE_ENV_EXISTENTE"
	const nova = "ECOMMERCE_TESTE_ENV_NOVA"
	t.Setenv(existente, "terminal")
	valorAnterior, definida := os.LookupEnv(nova)
	_ = os.Unsetenv(nova)
	t.Cleanup(func() {
		if definida {
			_ = os.Setenv(nova, valorAnterior)
		} else {
			_ = os.Unsetenv(nova)
		}
	})
	conteudo := existente + "=arquivo\n" + nova + "='valor com espaços'\n"
	if err := os.WriteFile(caminho, []byte(conteudo), 0600); err != nil {
		t.Fatal(err)
	}
	if err := CarregarEnv(caminho); err != nil {
		t.Fatal(err)
	}
	if os.Getenv(existente) != "terminal" || os.Getenv(nova) != "valor com espaços" {
		t.Fatal("precedência ou leitura do .env incorreta")
	}
}

func TestCarregarEnvAusenteOuInvalido(t *testing.T) {
	caminho := filepath.Join(t.TempDir(), ".env")
	if err := CarregarEnv(caminho); err != nil {
		t.Fatalf("arquivo ausente deve ser opcional: %v", err)
	}
	if err := os.WriteFile(caminho, []byte("segredo-que-nao-pode-aparecer@=teste"), 0600); err != nil {
		t.Fatal(err)
	}
	err := CarregarEnv(caminho)
	if err == nil || strings.Contains(err.Error(), "segredo-que-nao-pode-aparecer") {
		t.Fatalf("erro inválido ou revelando conteúdo: %v", err)
	}
}
