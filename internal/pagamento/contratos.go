// Package pagamento coordena cobranças sem depender de HTTP ou RabbitMQ.
package pagamento

import (
	"errors"
	"math"
	"net/url"
	"strings"
)

const (
	Pendente = "PENDENTE"
	Aprovado = "APROVADO"
	Recusado = "RECUSADO"
)

var (
	ErrInvalido      = errors.New("dados inválidos")
	ErrNaoEncontrado = errors.New("cobrança não encontrada")
	ErrConflito      = errors.New("cobrança já possui outro valor ou resultado")
	ErrNaoAutorizado = errors.New("webhook não autorizado")
)

// CriarCobranca é o contrato REST entre o MS Pagamento e o provedor externo.
// O token é conhecido apenas pelos dois servidores, nunca pelo navegador.
type CriarCobranca struct {
	PedidoID     string  `json:"pedido_id"`
	ValorTotal   float64 `json:"valor_total"`
	WebhookURL   string  `json:"webhook_url"`
	WebhookToken string  `json:"webhook_token"`
}

type Cobranca struct {
	ID          string  `json:"id"`
	PedidoID    string  `json:"pedido_id"`
	ValorTotal  float64 `json:"valor_total"`
	CheckoutURL string  `json:"checkout_url"`
	Status      string  `json:"status"`
	Notificado  bool    `json:"notificado"`
}

type Resultado struct {
	CobrancaID string `json:"cobranca_id"`
	PedidoID   string `json:"pedido_id"`
	Status     string `json:"status"`
}

func ValorValido(valor float64) bool {
	return valor > 0 && !math.IsNaN(valor) && !math.IsInf(valor, 0) &&
		math.Abs(valor*100-math.Round(valor*100)) < 0.000001
}

func URLValida(endereco string) bool {
	u, err := url.Parse(endereco)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" &&
		u.User == nil && u.Fragment == ""
}

func (c CriarCobranca) Validar() error {
	if strings.TrimSpace(c.PedidoID) == "" || !ValorValido(c.ValorTotal) ||
		!URLValida(c.WebhookURL) || c.WebhookToken == "" {
		return ErrInvalido
	}
	return nil
}
