package pagamento_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bdsromulo/Trab2-SistDistribuidos/internal/events"
	"github.com/bdsromulo/Trab2-SistDistribuidos/internal/execucao"
	"github.com/bdsromulo/Trab2-SistDistribuidos/internal/mockpagamento"
	"github.com/bdsromulo/Trab2-SistDistribuidos/internal/pagamento"
)

type publicado struct {
	tipo  string
	dados any
}
type cenario struct {
	pagamento           *pagamento.Servico
	mock                *mockpagamento.Servico
	urlMock, urlWebhook string
	mu                  sync.Mutex
	eventos             []publicado
	falharResultado     bool
}

func novoCenario(t *testing.T) *cenario {
	t.Helper()
	c := &cenario{}
	servidorPagamento := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pagamento.Handler(c.pagamento).ServeHTTP(w, r)
	}))
	t.Cleanup(servidorPagamento.Close)
	c.urlWebhook = servidorPagamento.URL + "/webhooks/pagamento"
	c.mock = mockpagamento.NovoServico("", c.urlWebhook, execucao.ClienteHTTP())
	servidorMock := httptest.NewServer(mockpagamento.Handler(c.mock, "token-teste"))
	t.Cleanup(servidorMock.Close)
	c.urlMock = servidorMock.URL
	c.mock.URLPublica = servidorMock.URL
	c.pagamento = pagamento.NovoServico(&pagamento.ClienteMock{
		URL: servidorMock.URL, Token: "token-teste", HTTP: execucao.ClienteHTTP(),
	}, c.urlWebhook, func(_ context.Context, tipo string, dados any) error {
		c.mu.Lock()
		defer c.mu.Unlock()
		if c.falharResultado && tipo != events.PagamentoCriado {
			c.falharResultado = false
			return errors.New("broker temporariamente indisponível")
		}
		c.eventos = append(c.eventos, publicado{tipo, dados})
		return nil
	})
	return c
}

func (c *cenario) publicados() []publicado {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]publicado(nil), c.eventos...)
}

func (c *cenario) criar(t *testing.T) events.PagamentoCriadoDados {
	t.Helper()
	dados := events.PedidoEstoqueOKDados{PedidoID: "pedido-42", ValorTotal: 199.90}
	for i := 0; i < 2; i++ {
		if err := c.pagamento.EstoqueConfirmado(context.Background(), dados); err != nil {
			t.Fatal(err)
		}
	}
	publicados := c.publicados()
	if len(publicados) != 1 || publicados[0].tipo != events.PagamentoCriado {
		t.Fatalf("eventos = %+v", publicados)
	}
	return publicados[0].dados.(events.PagamentoCriadoDados)
}

func requisicao(t *testing.T, metodo, url, corpo, token string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(metodo, url, strings.NewReader(corpo))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := execucao.ClienteHTTP().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	bruto, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, string(bruto)
}

func TestFluxoHTTPDoCheckoutAteEventoCompativelComEntrega(t *testing.T) {
	for _, status := range []string{pagamento.Aprovado, pagamento.Recusado} {
		t.Run(status, func(t *testing.T) {
			c := novoCenario(t)
			criado := c.criar(t)
			codigo, html := requisicao(t, "GET", criado.CheckoutURL, "", "")
			if codigo != 200 || !strings.Contains(html, "Pagamento Aprovado") || !strings.Contains(html, "Pagamento Recusado") {
				t.Fatalf("checkout HTTP %d: %s", codigo, html)
			}
			url := c.urlMock + "/api/cobrancas/" + criado.CobrancaID
			codigo, corpo := requisicao(t, "GET", url, "", "")
			if codigo != 200 || strings.Contains(corpo, "webhook_token") || strings.Contains(corpo, "webhook_url") {
				t.Fatalf("dados públicos inválidos: HTTP %d %s", codigo, corpo)
			}
			for i := 0; i < 2; i++ {
				codigo, corpo = requisicao(t, "POST", url+"/resultado", fmt.Sprintf(`{"status":%q}`, status), "")
				if codigo != 200 {
					t.Fatalf("resultado HTTP %d: %s", codigo, corpo)
				}
			}
			publicados := c.publicados()
			if len(publicados) != 2 {
				t.Fatalf("eventos duplicados: %+v", publicados)
			}
			if status == pagamento.Aprovado {
				// Estes são exatamente os campos que a Entrega existente lê.
				bruto, _ := json.Marshal(publicados[1].dados)
				var dados events.PagamentoAprovadoDados
				if err := json.Unmarshal(bruto, &dados); err != nil {
					t.Fatal(err)
				}
				if publicados[1].tipo != events.PagamentoAprovado || dados.PedidoID != "pedido-42" || dados.ValorTotal != 199.90 {
					t.Fatalf("contrato da Entrega alterado: %+v", publicados[1])
				}
			} else if publicados[1].tipo != events.PagamentoRecusado {
				t.Fatalf("evento = %+v", publicados[1])
			}
			oposto := pagamento.Aprovado
			if status == pagamento.Aprovado {
				oposto = pagamento.Recusado
			}
			codigo, _ = requisicao(t, "POST", url+"/resultado", fmt.Sprintf(`{"status":%q}`, oposto), "")
			if codigo != http.StatusConflict {
				t.Fatalf("alterar decisão: HTTP %d", codigo)
			}
		})
	}
}

func TestFalhaNoBrokerPermiteReenviarMesmaEscolha(t *testing.T) {
	c := novoCenario(t)
	criado := c.criar(t)
	c.mu.Lock()
	c.falharResultado = true
	c.mu.Unlock()
	url := c.urlMock + "/api/cobrancas/" + criado.CobrancaID + "/resultado"
	codigo, _ := requisicao(t, "POST", url, `{"status":"APROVADO"}`, "")
	if codigo != http.StatusBadGateway {
		t.Fatalf("falha deveria permitir retry: HTTP %d", codigo)
	}
	cobranca, _ := c.mock.Buscar(criado.CobrancaID)
	if cobranca.Notificado || cobranca.Status != pagamento.Aprovado {
		t.Fatalf("decisão não foi preservada: %+v", cobranca)
	}
	codigo, _ = requisicao(t, "POST", url, `{"status":"RECUSADO"}`, "")
	if codigo != http.StatusConflict {
		t.Fatalf("decisão conflitante aceita: HTTP %d", codigo)
	}
	codigo, corpo := requisicao(t, "POST", url, `{"status":"APROVADO"}`, "")
	if codigo != 200 || len(c.publicados()) != 2 {
		t.Fatalf("retry HTTP %d: %s; eventos %+v", codigo, corpo, c.publicados())
	}
}

func TestWebhookRejeitaTokenInvalidoEPedidoDiferente(t *testing.T) {
	c := novoCenario(t)
	criado := c.criar(t)
	corpo := fmt.Sprintf(`{"cobranca_id":%q,"pedido_id":"pedido-42","status":"APROVADO"}`, criado.CobrancaID)
	codigo, _ := requisicao(t, "POST", c.urlWebhook, corpo, "token-falso")
	if codigo != 401 {
		t.Fatalf("token inválido: HTTP %d", codigo)
	}
	corpo = fmt.Sprintf(`{"cobranca_id":%q,"pedido_id":"outro-pedido","status":"APROVADO"}`, criado.CobrancaID)
	codigo, _ = requisicao(t, "POST", c.urlWebhook, corpo, "token-falso")
	if codigo != 404 || len(c.publicados()) != 1 {
		t.Fatalf("pedido errado aceito: HTTP %d", codigo)
	}
}

func TestMockValidaCredencialCallbackEValor(t *testing.T) {
	c := novoCenario(t)
	base := pagamento.CriarCobranca{PedidoID: "42", ValorTotal: 10, WebhookURL: c.urlWebhook, WebhookToken: "secreto"}
	for _, caso := range []struct {
		nome    string
		alterar func(*pagamento.CriarCobranca)
		token   string
		codigo  int
	}{
		{"sem credencial", func(*pagamento.CriarCobranca) {}, "", 401},
		{"valor negativo", func(d *pagamento.CriarCobranca) { d.ValorTotal = -1 }, "token-teste", 400},
		{"fração de centavo", func(d *pagamento.CriarCobranca) { d.ValorTotal = 1.001 }, "token-teste", 400},
		{"callback desconhecido", func(d *pagamento.CriarCobranca) { d.WebhookURL = "http://outro.example/webhook" }, "token-teste", 400},
	} {
		t.Run(caso.nome, func(t *testing.T) {
			d := base
			caso.alterar(&d)
			bruto, _ := json.Marshal(d)
			codigo, _ := requisicao(t, "POST", c.urlMock+"/api/cobrancas", string(bruto), caso.token)
			if codigo != caso.codigo {
				t.Fatalf("HTTP %d, esperado %d", codigo, caso.codigo)
			}
		})
	}
	bruto, _ := json.Marshal(base)
	var primeira pagamento.Cobranca
	for i := 0; i < 2; i++ {
		codigo, corpo := requisicao(t, "POST", c.urlMock+"/api/cobrancas", string(bruto), "token-teste")
		if codigo != 201 {
			t.Fatalf("HTTP %d: %s", codigo, corpo)
		}
		var atual pagamento.Cobranca
		if err := json.Unmarshal([]byte(corpo), &atual); err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			primeira = atual
		} else if atual.ID != primeira.ID {
			t.Fatal("cobrança duplicada")
		}
	}
	codigo, _ := requisicao(t, "POST", c.urlMock+"/api/cobrancas", string(bruto)+` {}`, "token-teste")
	if codigo != 400 {
		t.Fatalf("dois objetos JSON aceitos: HTTP %d", codigo)
	}
}

type provedorLento struct {
	iniciou chan struct{}
	liberar chan struct{}
}

func (p *provedorLento) Criar(ctx context.Context, d pagamento.CriarCobranca) (pagamento.Cobranca, error) {
	if d.PedidoID == "lento" {
		close(p.iniciou)
		select {
		case <-p.liberar:
		case <-ctx.Done():
			return pagamento.Cobranca{}, ctx.Err()
		}
	}
	return pagamento.Cobranca{
		ID: d.PedidoID, PedidoID: d.PedidoID, ValorTotal: d.ValorTotal,
		CheckoutURL: "http://mock.example/checkout/" + d.PedidoID, Status: pagamento.Pendente,
	}, nil
}

func TestCobrancaLentaNaoBloqueiaOutroPedido(t *testing.T) {
	p := &provedorLento{iniciou: make(chan struct{}), liberar: make(chan struct{})}
	s := pagamento.NovoServico(p, "http://pagamento.example/webhooks/pagamento", func(context.Context, string, any) error { return nil })
	ctx, cancelar := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelar()
	lento := make(chan error, 1)
	go func() {
		lento <- s.EstoqueConfirmado(ctx, events.PedidoEstoqueOKDados{PedidoID: "lento", ValorTotal: 10})
	}()
	<-p.iniciou
	rapido := make(chan error, 1)
	go func() {
		rapido <- s.EstoqueConfirmado(ctx, events.PedidoEstoqueOKDados{PedidoID: "rapido", ValorTotal: 20})
	}()
	select {
	case err := <-rapido:
		if err != nil {
			t.Error(err)
		}
	case <-time.After(time.Second):
		t.Error("pedido rápido bloqueado pela chamada do outro pedido")
	}
	close(p.liberar)
	if err := <-lento; err != nil {
		t.Fatal(err)
	}
}

type respostaPerdida struct {
	provedor pagamento.Provedor
	primeira bool
}

func (p *respostaPerdida) Criar(ctx context.Context, d pagamento.CriarCobranca) (pagamento.Cobranca, error) {
	c, err := p.provedor.Criar(ctx, d)
	if err == nil && !p.primeira {
		p.primeira = true
		return pagamento.Cobranca{}, errors.New("resposta perdida após o Mock criar a cobrança")
	}
	return c, err
}

func TestRetryPreservaTokenQuandoRespostaDaCriacaoSePerde(t *testing.T) {
	c := novoCenario(t)
	provedor := &respostaPerdida{provedor: &pagamento.ClienteMock{URL: c.urlMock, Token: "token-teste", HTTP: execucao.ClienteHTTP()}}
	var anuncios int
	s := pagamento.NovoServico(provedor, c.urlWebhook, func(context.Context, string, any) error { anuncios++; return nil })
	dados := events.PedidoEstoqueOKDados{PedidoID: "retry", ValorTotal: 10}
	if err := s.EstoqueConfirmado(context.Background(), dados); err == nil {
		t.Fatal("falha de rede deveria ser propagada")
	}
	if err := s.EstoqueConfirmado(context.Background(), dados); err != nil {
		t.Fatalf("retry deveria reutilizar a cobrança: %v", err)
	}
	if anuncios != 1 {
		t.Fatalf("anúncios = %d", anuncios)
	}
}
