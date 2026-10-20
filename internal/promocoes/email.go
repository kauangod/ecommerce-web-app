package promocoes

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"

	"github.com/bdsromulo/Trab2-SistDistribuidos/internal/events"
)

// EnviadorEmail permite testar a seleção por categoria sem enviar mensagens.
type EnviadorEmail interface {
	Enviar(context.Context, string, string, events.PromocaoDados) error
}

type Notificador struct {
	Interesses *Interesses
	Email      EnviadorEmail
	mu         sync.Mutex
	enviados   map[string]map[string]bool
}

// Notificar é usado pelo consumidor da fila de e-mails. Uma falha faz a
// mensagem voltar à fila; o eventID mantém a mesma chave no retry da Resend.
func (n *Notificador) Notificar(ctx context.Context, eventID string, promo events.PromocaoDados) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.enviados == nil {
		n.enviados = make(map[string]map[string]bool)
	}
	if n.enviados[eventID] == nil {
		n.enviados[eventID] = make(map[string]bool)
	}
	for _, email := range n.Interesses.Destinatarios(promo.Categoria) {
		if n.enviados[eventID][email] {
			continue
		}
		if err := n.Email.Enviar(ctx, eventID, email, promo); err != nil {
			return err
		}
		n.enviados[eventID][email] = true
	}
	delete(n.enviados, eventID)
	return nil
}

type Resend struct {
	APIKey    string
	Remetente string
	Endpoint  string
	HTTP      *http.Client
}

// Enviar segue POST /emails da Resend. Cada destinatário recebe uma chamada
// separada para não expor os endereços dos outros clientes.
func (c *Resend) Enviar(ctx context.Context, eventID, email string, promo events.PromocaoDados) error {
	texto := fmt.Sprintf("Promoção da categoria %s\n\n%s\nDe R$ %s por R$ %s (%d%% de desconto).\n\nVocê recebe esta oferta porque cadastrou interesse nesta categoria. Para cancelar, atualize suas preferências na loja.",
		promo.Categoria, promo.Nome, reais(promo.PrecoOriginal), reais(promo.PrecoPromocional), promo.Desconto)
	return c.enviarMensagem(ctx, eventID, email, "Oferta: "+promo.Nome, texto)
}

func (c *Resend) EnviarTeste(ctx context.Context, eventID, email string) error {
	return c.enviarMensagem(ctx, eventID, email,
		"Teste de integração do e-commerce com a Resend",
		"Olá!\n\nEste é um e-mail de teste da integração do MS Promoções com a API Resend.\n\nSeu endereço foi informado na lista local de destinatários de teste.\n\nEnviado pelo comando go run ./cmd/testar-email.")
}

func (c *Resend) enviarMensagem(ctx context.Context, eventID, email, assunto, texto string) error {
	if c.APIKey == "" || c.Remetente == "" {
		return fmt.Errorf("configure RESEND_API_KEY e PROMOCOES_EMAIL_FROM")
	}
	dados := struct {
		From    string   `json:"from"`
		To      []string `json:"to"`
		Subject string   `json:"subject"`
		Text    string   `json:"text"`
	}{
		From: c.Remetente, To: []string{email}, Subject: assunto, Text: texto,
	}
	bruto, err := json.Marshal(dados)
	if err != nil {
		return err
	}
	endpoint := c.Endpoint
	if endpoint == "" {
		endpoint = "https://api.resend.com/emails"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(bruto))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	req.Header.Set("Idempotency-Key", fmt.Sprintf("promocao/%x", sha256.Sum256([]byte(eventID+"/"+email))))
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var falha struct {
			Message string `json:"message"`
		}
		if err := json.NewDecoder(io.LimitReader(resp.Body, 16<<10)).Decode(&falha); err == nil && falha.Message != "" {
			mensagem := strings.ReplaceAll(falha.Message, c.APIKey, "[chave oculta]")
			return fmt.Errorf("Resend respondeu HTTP %d: %s", resp.StatusCode, mensagem)
		}
		return fmt.Errorf("Resend respondeu HTTP %d", resp.StatusCode)
	}
	var resposta struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 16<<10)).Decode(&resposta); err != nil || resposta.ID == "" {
		return fmt.Errorf("resposta inválida da Resend")
	}
	return nil
}

func reais(valor float64) string { return strings.ReplaceAll(fmt.Sprintf("%.2f", valor), ".", ",") }
