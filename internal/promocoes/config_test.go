package promocoes

import (
	"reflect"
	"strings"
	"testing"
)

func TestConfigEmailLeVetorDeStrings(t *testing.T) {
	t.Setenv("RESEND_API_KEY", "chave-local-de-teste")
	t.Setenv("PROMOCOES_EMAIL_FROM", "Loja <onboarding@resend.dev>")
	t.Setenv("PROMOCOES_EMAILS_TESTE", `[" Pessoa@Example.com ","outro@example.com","pessoa@example.com"]`)
	c, err := CarregarConfigEmail()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(c.DestinatariosTeste, []string{"pessoa@example.com", "outro@example.com"}) {
		t.Fatalf("lista normalizada = %v", c.DestinatariosTeste)
	}
}

func TestConfigEmailRejeitaListaInvalidaSemExporChave(t *testing.T) {
	t.Setenv("RESEND_API_KEY", "chave-local-de-teste")
	t.Setenv("PROMOCOES_EMAIL_FROM", "Loja <onboarding@resend.dev>")
	for _, lista := range []string{`pessoa@example.com`, `["endereco-invalido"]`, `["Pessoa <pessoa@example.com>"]`, `[42]`} {
		t.Run(lista, func(t *testing.T) {
			t.Setenv("PROMOCOES_EMAILS_TESTE", lista)
			_, err := CarregarConfigEmail()
			if err == nil || strings.Contains(err.Error(), "chave-local-de-teste") {
				t.Fatalf("erro de configuração inválido: %v", err)
			}
		})
	}
}
