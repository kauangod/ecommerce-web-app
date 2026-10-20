package main

import (
	"errors"
	"net/mail"
	"strings"

	"github.com/bdsromulo/Trab2-SistDistribuidos/internal/catalogo"
	"github.com/bdsromulo/Trab2-SistDistribuidos/internal/events"
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/sse"
	"github.com/gofiber/fiber/v3/middleware/static"
)

type ProdutoHTTP struct {
	ID           string  `json:"id"`
	Availability int     `json:"availability"`
	Nome         string  `json:"nome"`
	Categoria    string  `json:"categoria"`
	Preco        float64 `json:"preco"`
}

func configurarHTTP(servico *Servico, pedidos *Pedidos, produtos []catalogo.Produto, hub *HubSSE) *fiber.App {
	app := fiber.New()

	app.Get("/produtos", func(c fiber.Ctx) error {
		estoque, err := listarEstoque()
		if err != nil {
			return c.Status(fiber.StatusBadGateway).JSON(fiber.Map{"erro": err.Error()})
		}
		disponibilidade := make(map[string]int, len(estoque))
		for _, item := range estoque {
			disponibilidade[strings.ToUpper(item.ID)] = item.Availability
		}
		lista := make([]ProdutoHTTP, 0, len(produtos))
		for _, produto := range produtos {
			lista = append(lista, ProdutoHTTP{
				ID: produto.ID, Availability: disponibilidade[strings.ToUpper(produto.ID)],
				Nome: produto.Nome, Categoria: produto.Categoria, Preco: produto.Preco,
			})
		}
		return c.JSON(lista)
	})

	app.Get("/pedidos", func(c fiber.Ctx) error {
		if email := strings.TrimSpace(c.Query("email")); email != "" {
			email, ok := normalizarEmail(email)
			if !ok {
				return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"erro": "e-mail inválido"})
			}
			return c.JSON(pedidos.ListarPorEmail(email))
		}
		return c.JSON(pedidos.Listar())
	})

	app.Post("/pedidos", func(c fiber.Ctx) error {
		var req struct {
			Email string `json:"email"`
			Itens []struct {
				ProdutoID  string `json:"produto_id"`
				Quantidade int    `json:"quantidade"`
			} `json:"itens"`
		}
		if err := c.Bind().Body(&req); err != nil || len(req.Itens) == 0 {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"erro": "body inválido ou sem itens"})
		}
		email, ok := normalizarEmail(req.Email)
		if !ok {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"erro": "e-mail inválido"})
		}

		itens := make([]events.Item, 0, len(req.Itens))
		for _, item := range req.Itens {
			produto, ok := buscarProdutoCatalogo(produtos, item.ProdutoID)
			if !ok || item.Quantidade <= 0 {
				return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"erro": "body inválido ou sem itens"})
			}
			itens = append(itens, events.Item{
				ProdutoID: item.ProdutoID, Nome: produto.Nome,
				Quantidade: item.Quantidade, PrecoUnitario: produto.Preco,
			})
		}

		pedido, err := servico.CriarPedidoPara(email, itens)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"erro": "falha ao criar pedido"})
		}
		return c.Status(fiber.StatusCreated).JSON(pedido)
	})

	app.Delete("/pedidos/:id", func(c fiber.Ctx) error {
		if err := servico.ExcluirPeloUsuario(c.Params("id")); err != nil {
			status := fiber.StatusConflict
			if errors.Is(err, ErrPedidoNaoEncontrado) {
				status = fiber.StatusNotFound
			}
			return c.Status(status).JSON(fiber.Map{"erro": err.Error()})
		}
		return c.SendStatus(fiber.StatusAccepted)
	})

	app.Put("/interesses/:categoria", func(c fiber.Ctx) error {
		categoria := strings.ToUpper(c.Params("categoria"))
		var req struct {
			Email string `json:"email"`
		}
		if err := c.Bind().Body(&req); err != nil || strings.TrimSpace(req.Email) == "" || !categoriaValida(categoria) {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"erro": "e-mail e categoria válida são obrigatórios"})
		}
		if err := Registrar(events.InteresseCategoria{Categoria: categoria, Email: strings.TrimSpace(req.Email)}); err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"erro": "falha ao registrar interesse"})
		}
		return c.SendStatus(fiber.StatusAccepted)
	})

	app.Delete("/interesses/:categoria", func(c fiber.Ctx) error {
		categoria := strings.ToUpper(c.Params("categoria"))
		email := strings.TrimSpace(c.Query("email"))
		if email == "" || !categoriaValida(categoria) {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"erro": "e-mail e categoria válida são obrigatórios"})
		}
		if err := Cancelar(events.InteresseCategoria{Categoria: categoria, Email: email}); err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"erro": "falha ao cancelar interesse"})
		}
		return c.SendStatus(fiber.StatusAccepted)
	})

	eventos := sse.New(sse.Config{Handler: hub.Handler})
	app.Get("/eventos", func(c fiber.Ctx) error {
		if _, ok := normalizarEmail(c.Query("email")); !ok {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"erro": "e-mail inválido"})
		}
		return eventos(c)
	})
	app.Get("/*", static.New("./frontend"))
	return app
}

func categoriaValida(categoria string) bool {
	return categoria == "A" || categoria == "B" || categoria == "C"
}

func normalizarEmail(valor string) (string, bool) {
	email := strings.ToLower(strings.TrimSpace(valor))
	endereco, err := mail.ParseAddress(email)
	return email, err == nil && endereco.Address == email
}
