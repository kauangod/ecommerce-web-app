package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/bdsromulo/Trab2-SistDistribuidos/internal/events"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestNotificacoesPedidoEEventosIgnorados(t *testing.T) {
	pedidos := NovosPedidos()
	conexoes := NovasConexoes()
	a := conexoes.Abrir("a@example.com")
	b := conexoes.Abrir("b@example.com")
	s := NovoServico(pedidos, func(string, any) error { return nil }, func(string) {}, func(p Pedido) {
		data, err := json.Marshal(p)
		if err != nil {
			t.Fatal(err)
		}
		conexoes.Notificar(p.Cliente, string(data))
	})
	s.janela = time.Hour
	if _, err := s.CriarPedido(itensTeste, ""); !errors.Is(err, ErrClienteObrigatorio) {
		t.Fatalf("cliente obrigatório: %v", err)
	}
	p, err := s.CriarPedido(itensTeste, "a@example.com")
	if err != nil {
		t.Fatal(err)
	}
	checkout := evento(t, events.PagamentoCriado, events.PagamentoCriadoDados{PedidoID: p.ID, CobrancaID: "c1", CheckoutURL: "https://checkout"})
	tratar(t, s, checkout)
	var recebido Pedido
	select {
	case msg := <-a:
		if err := json.Unmarshal([]byte(msg), &recebido); err != nil {
			t.Fatal(err)
		}
	default:
		t.Fatal("notificação ausente")
	}
	if recebido.Cliente != p.Cliente || recebido.CheckoutURL != "https://checkout" || recebido.CobrancaID != "c1" {
		t.Fatalf("payload: %+v", recebido)
	}
	tratar(t, s, checkout)
	tratar(t, s, evento(t, events.PedidoEstoqueOK, events.PedidoEstoqueOKDados{PedidoID: p.ID}))
	select {
	case msg := <-a:
		if err := json.Unmarshal([]byte(msg), &recebido); err != nil {
			t.Fatal(err)
		}
		if recebido.Status != StatusAguardandoPagto || recebido.CheckoutURL != "https://checkout" {
			t.Fatalf("payload: %+v", recebido)
		}
	default:
		t.Fatal("status ausente")
	}
	tratar(t, s, evento(t, events.PedidoEstoqueOK, events.PedidoEstoqueOKDados{PedidoID: p.ID}))
	select {
	case <-a:
		t.Fatal("evento repetido notificado")
	default:
	}
	select {
	case <-b:
		t.Fatal("notificação vazou para outro cliente")
	default:
	}
	conexoes.Fechar("a@example.com", a)
	if _, ok := <-a; ok {
		t.Fatal("canal não fechado")
	}
	conexoes.Notificar("a@example.com", "depois")
	conexoes.Fechar("b@example.com", b)
}
