package main

import (
	"sync"

	"github.com/bdsromulo/Trab2-SistDistribuidos/internal/events"
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/sse"
)

type HubSSE struct {
	mu       sync.Mutex
	pedidos  *Pedidos
	clientes map[chan events.Envelope]string
}

func NovoHubSSE(pedidos *Pedidos) *HubSSE {
	return &HubSSE{pedidos: pedidos, clientes: make(map[chan events.Envelope]string)}
}

func (h *HubSSE) Publicar(env events.Envelope) {
	var dados struct {
		PedidoID string `json:"pedido_id"`
	}
	if env.Decodificar(&dados) != nil || dados.PedidoID == "" {
		return
	}
	pedido, ok := h.pedidos.Buscar(dados.PedidoID)
	if !ok || pedido.Email == "" {
		return
	}
	if pedido.Status == StatusExcluido && env.EventType != events.EstoqueIndisponivel && env.EventType != events.PagamentoRecusado {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for cliente, email := range h.clientes {
		if email != pedido.Email {
			continue
		}
		select {
		case cliente <- env:
		default:
		}
	}
}

func (h *HubSSE) Handler(c fiber.Ctx, stream *sse.Stream) error {
	cliente := make(chan events.Envelope, 16)
	email, _ := normalizarEmail(c.Query("email"))
	h.mu.Lock()
	h.clientes[cliente] = email
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		delete(h.clientes, cliente)
		h.mu.Unlock()
	}()

	if err := stream.Comment("conectado"); err != nil {
		return err
	}
	for {
		select {
		case env := <-cliente:
			if err := stream.Event(sse.Event{ID: env.EventID, Name: env.EventType, Data: env.Data}); err != nil {
				return err
			}
		case <-stream.Done():
			return stream.Err()
		}
	}
}
