package promocoes

import (
	"encoding/json"
	"fmt"
	"net/mail"
	"os"
	"slices"
	"strings"
)

type ConfigEmail struct {
	APIKey             string
	Remetente          string
	DestinatariosTeste []string
}

// CarregarConfigEmail converte o vetor JSON do .env em um slice de strings.
// Essa lista é usada somente pelo comando de teste; as promoções normais
// continuam respeitando as inscrições e categorias recebidas pelo RabbitMQ.
func CarregarConfigEmail() (ConfigEmail, error) {
	c := ConfigEmail{
		APIKey:    strings.TrimSpace(os.Getenv("RESEND_API_KEY")),
		Remetente: strings.TrimSpace(os.Getenv("PROMOCOES_EMAIL_FROM")),
	}
	if c.APIKey == "" || c.Remetente == "" {
		return c, fmt.Errorf("preencha RESEND_API_KEY e PROMOCOES_EMAIL_FROM no arquivo .env")
	}
	if _, err := mail.ParseAddress(c.Remetente); err != nil {
		return c, fmt.Errorf("PROMOCOES_EMAIL_FROM deve conter um remetente válido")
	}
	lista := strings.TrimSpace(os.Getenv("PROMOCOES_EMAILS_TESTE"))
	if lista == "" {
		return c, nil
	}
	var emails []string
	if err := json.Unmarshal([]byte(lista), &emails); err != nil {
		return c, fmt.Errorf("PROMOCOES_EMAILS_TESTE deve ser um vetor JSON de strings")
	}
	for _, email := range emails {
		email = strings.ToLower(strings.TrimSpace(email))
		endereco, err := mail.ParseAddress(email)
		if err != nil || endereco.Address != email {
			return c, fmt.Errorf("PROMOCOES_EMAILS_TESTE contém um endereço inválido")
		}
		if !slices.Contains(c.DestinatariosTeste, email) {
			c.DestinatariosTeste = append(c.DestinatariosTeste, email)
		}
	}
	return c, nil
}
