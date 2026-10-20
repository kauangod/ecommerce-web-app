// Package mockpagamento representa o provedor externo de pagamento.
package mockpagamento

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/bdsromulo/Trab2-SistDistribuidos/internal/pagamento"
	"github.com/google/uuid"
)

type registro struct {
	cobranca pagamento.Cobranca
	criacao  pagamento.CriarCobranca
}

type Servico struct {
	mu               sync.Mutex
	porID            map[string]*registro
	porPedido        map[string]string
	URLPublica       string
	WebhookPermitido string
	HTTP             *http.Client
}

func NovoServico(urlPublica, webhookPermitido string, cliente *http.Client) *Servico {
	return &Servico{
		porID: make(map[string]*registro), porPedido: make(map[string]string),
		URLPublica: strings.TrimRight(urlPublica, "/"), WebhookPermitido: webhookPermitido, HTTP: cliente,
	}
}

// Criar usa pedido_id como chave de idempotência: retries com os mesmos
// dados recebem a mesma URL, e dados diferentes são recusados.
func (s *Servico) Criar(dados pagamento.CriarCobranca) (pagamento.Cobranca, error) {
	if err := dados.Validar(); err != nil || dados.WebhookURL != s.WebhookPermitido {
		return pagamento.Cobranca{}, pagamento.ErrInvalido
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if id, ok := s.porPedido[dados.PedidoID]; ok {
		r := s.porID[id]
		if r.criacao != dados {
			return pagamento.Cobranca{}, pagamento.ErrConflito
		}
		return r.cobranca, nil
	}
	id := uuid.NewString()
	c := pagamento.Cobranca{
		ID: id, PedidoID: dados.PedidoID, ValorTotal: dados.ValorTotal,
		CheckoutURL: s.URLPublica + "/checkout/" + id, Status: pagamento.Pendente,
	}
	s.porID[id] = &registro{cobranca: c, criacao: dados}
	s.porPedido[dados.PedidoID] = id
	return c, nil
}

func (s *Servico) Buscar(id string) (pagamento.Cobranca, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.porID[id]
	if !ok {
		return pagamento.Cobranca{}, pagamento.ErrNaoEncontrado
	}
	return r.cobranca, nil
}

// Escolher mantém a primeira decisão, mesmo se a resposta do webhook se
// perder. O botão pode repetir essa decisão até o MS confirmar o recebimento.
func (s *Servico) Escolher(ctx context.Context, id, status string) (pagamento.Cobranca, error) {
	if status != pagamento.Aprovado && status != pagamento.Recusado {
		return pagamento.Cobranca{}, pagamento.ErrInvalido
	}
	s.mu.Lock()
	r, ok := s.porID[id]
	if !ok {
		s.mu.Unlock()
		return pagamento.Cobranca{}, pagamento.ErrNaoEncontrado
	}
	if r.cobranca.Status != pagamento.Pendente && r.cobranca.Status != status {
		c := r.cobranca
		s.mu.Unlock()
		return c, pagamento.ErrConflito
	}
	if r.cobranca.Notificado {
		c := r.cobranca
		s.mu.Unlock()
		return c, nil
	}
	r.cobranca.Status = status
	cobranca, criacao := r.cobranca, r.criacao
	s.mu.Unlock()
	// A escolha fica salva, mas a chamada ao outro servidor acontece sem
	// segurar o mutex do catálogo de cobranças.
	bruto, err := json.Marshal(pagamento.Resultado{CobrancaID: id, PedidoID: cobranca.PedidoID, Status: status})
	if err != nil {
		return cobranca, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, criacao.WebhookURL, bytes.NewReader(bruto))
	if err != nil {
		return cobranca, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+criacao.WebhookToken)
	resp, err := s.HTTP.Do(req)
	if err != nil {
		return cobranca, fmt.Errorf("enviar webhook: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return cobranca, fmt.Errorf("webhook respondeu HTTP %d", resp.StatusCode)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	r.cobranca.Notificado = true
	return r.cobranca, nil
}
