package pagamento

import (
	"context"
	"crypto/subtle"
	"fmt"
	"strings"
	"sync"

	"github.com/bdsromulo/Trab2-SistDistribuidos/internal/events"
	"github.com/google/uuid"
)

// Provedor é implementado pelo cliente REST. Nos testes usamos o Mock real
// com httptest, sem precisar subir o RabbitMQ.
type Provedor interface {
	Criar(context.Context, CriarCobranca) (Cobranca, error)
}

type registro struct {
	mu        sync.Mutex
	cobranca  Cobranca
	token     string
	anunciado bool
}

type Servico struct {
	mu         sync.Mutex
	porPedido  map[string]*registro
	provedor   Provedor
	webhookURL string
	publicar   func(context.Context, string, any) error
}

func NovoServico(provedor Provedor, webhookURL string, publicar func(context.Context, string, any) error) *Servico {
	return &Servico{porPedido: make(map[string]*registro), provedor: provedor, webhookURL: webhookURL, publicar: publicar}
}

// EstoqueConfirmado cria uma única cobrança por pedido. Se anunciar a URL
// falhar, a reentrega do evento tenta anunciar a mesma cobrança novamente.
func (s *Servico) EstoqueConfirmado(ctx context.Context, dados events.PedidoEstoqueOKDados) error {
	if strings.TrimSpace(dados.PedidoID) == "" || !ValorValido(dados.ValorTotal) {
		return ErrInvalido
	}
	s.mu.Lock()
	r, existe := s.porPedido[dados.PedidoID]
	if !existe {
		// O token deve ser estável durante retries de uma criação cuja resposta
		// HTTP se perdeu. Ele é guardado antes da chamada ao provedor.
		r = &registro{token: uuid.NewString(), cobranca: Cobranca{PedidoID: dados.PedidoID, ValorTotal: dados.ValorTotal}}
		s.porPedido[dados.PedidoID] = r
	}
	s.mu.Unlock()
	// Cada pedido tem seu próprio lock: uma chamada REST lenta não bloqueia
	// os webhooks e as cobranças dos outros clientes.
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cobranca.ValorTotal != dados.ValorTotal {
		return ErrConflito
	}
	if r.cobranca.ID == "" {
		cobranca, err := s.provedor.Criar(ctx, CriarCobranca{
			PedidoID: dados.PedidoID, ValorTotal: dados.ValorTotal,
			WebhookURL: s.webhookURL, WebhookToken: r.token,
		})
		if err != nil {
			return fmt.Errorf("criar cobrança: %w", err)
		}
		if cobranca.ID == "" || cobranca.PedidoID != dados.PedidoID ||
			cobranca.ValorTotal != dados.ValorTotal || cobranca.Status != Pendente || !URLValida(cobranca.CheckoutURL) {
			return fmt.Errorf("resposta inválida do provedor")
		}
		r.cobranca = cobranca
	}
	if r.anunciado {
		return nil
	}
	err := s.publicar(ctx, events.PagamentoCriado, events.PagamentoCriadoDados{
		PedidoID: dados.PedidoID, CobrancaID: r.cobranca.ID,
		ValorTotal: dados.ValorTotal, CheckoutURL: r.cobranca.CheckoutURL,
	})
	if err != nil {
		return err
	}
	r.anunciado = true
	return nil
}

// ReceberResultado usa o valor guardado na criação, nunca um valor enviado
// pelo navegador. Repetir o mesmo webhook não repete a publicação.
func (s *Servico) ReceberResultado(ctx context.Context, resultado Resultado, token string) error {
	if resultado.PedidoID == "" || resultado.CobrancaID == "" ||
		(resultado.Status != Aprovado && resultado.Status != Recusado) {
		return ErrInvalido
	}
	s.mu.Lock()
	r, ok := s.porPedido[resultado.PedidoID]
	s.mu.Unlock()
	if !ok {
		return ErrNaoEncontrado
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cobranca.ID == "" || r.cobranca.ID != resultado.CobrancaID {
		return ErrNaoEncontrado
	}
	if subtle.ConstantTimeCompare([]byte(token), []byte(r.token)) != 1 {
		return ErrNaoAutorizado
	}
	if r.cobranca.Status != Pendente && r.cobranca.Status != resultado.Status {
		return ErrConflito
	}
	if r.cobranca.Notificado {
		return nil
	}
	r.cobranca.Status = resultado.Status
	var err error
	if resultado.Status == Aprovado {
		err = s.publicar(ctx, events.PagamentoAprovado, events.PagamentoAprovadoDados{
			PedidoID: resultado.PedidoID, ValorTotal: r.cobranca.ValorTotal,
		})
	} else {
		err = s.publicar(ctx, events.PagamentoRecusado, events.PagamentoRecusadoDados{
			PedidoID: resultado.PedidoID, Motivo: "Pagamento recusado pelo usuário no Mock",
		})
	}
	if err != nil {
		return err // permite repetir o webhook após uma falha no RabbitMQ
	}
	r.cobranca.Notificado = true
	return nil
}
