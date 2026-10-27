// Microsserviço Principal: menu no terminal, pedidos e status.
//
// Duas goroutines trabalham juntas: a principal lê o menu no terminal e
// outra consome a fila.principal, atualizando o status dos pedidos.
package main

import (
	"bufio"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/mail"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/bdsromulo/Trab2-SistDistribuidos/internal/catalogo"
	e "github.com/bdsromulo/Trab2-SistDistribuidos/internal/events"
	s "github.com/bdsromulo/Trab2-SistDistribuidos/internal/signature"
	u "github.com/bdsromulo/Trab2-SistDistribuidos/internal/utils"
	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/static"
	uuid "github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"
)

func main() {
	produtos, err := catalogo.Carregar(catalogo.CaminhoPadrao)
	u.FailOnError(err, "Erro ao carregar o catálogo")

	conn, err := amqp.Dial("amqp://ecommerce:ecommerce@localhost:5672/")
	u.FailOnError(err, "Erro ao conectar ao RabbitMQ")
	defer conn.Close()

	ch, err := conn.Channel()
	u.FailOnError(err, "Erro ao abrir canal")
	defer ch.Close()

	err = ch.ExchangeDeclare(
		"eCommerce",
		"direct",
		true,
		false,
		false,
		false,
		nil,
	)
	u.FailOnError(err, "Erro ao declarar a exchange")

	// Interesses em categorias viajam na exchange de promoções (topic),
	// declarada com os mesmos parâmetros do microsserviço Promoções.
	err = ch.ExchangeDeclare(
		"Promocoes",
		"topic",
		true,
		false,
		false,
		false,
		nil,
	)
	u.FailOnError(err, "Erro ao declarar a exchange de promoções")

	_, err = ch.QueueDeclare(
		"fila.principal",
		true,
		false,
		false,
		false,
		nil,
	)
	u.FailOnError(err, "Erro ao declarar a fila")

	for _, rk := range []string{e.PedidoEstoqueOK, e.EstoqueIndisponivel, e.PagamentoCriado, e.PagamentoAprovado, e.PagamentoRecusado, e.PedidoEnviado} {
		err = ch.QueueBind(
			"fila.principal",
			rk,
			"eCommerce",
			false,
			nil,
		)
		u.FailOnError(err, "Erro ao bindar a fila "+rk)
	}

	pk := s.LoadPrivateKey("cmd/principal/keys/private_key.pem")
	if pk == nil {
		log.Fatal("Chave privada não encontrada em cmd/principal/keys/private_key.pem. " +
			"Gere as chaves uma única vez com: go run ./cmd/gerar-chaves")
	}

	// O Principal recebe eventos de três produtores: cada evento é conferido
	// com a chave pública de quem é o dono daquele tipo de evento.
	chavesPublicas := map[string]*rsa.PublicKey{
		e.Estoque:   s.LoadPublicKey("cmd/principal/estoque-pub/public_key.pem"),
		e.Pagamento: s.LoadPublicKey("cmd/principal/pagamento-pub/public_key.pem"),
		e.Entrega:   s.LoadPublicKey("cmd/principal/entrega-pub/public_key.pem"),
	}

	events, err := ch.Consume(
		"fila.principal",
		"MS_Principal",
		false,
		false,
		false,
		false,
		nil,
	)
	u.FailOnError(err, "Erro ao registrar o consumidor")

	// O menu e o consumo publicam no mesmo canal, cada um na sua goroutine.
	var mu sync.Mutex
	publicar := func(tipo string, dados any) error {
		mu.Lock()
		defer mu.Unlock()
		return publicarEvento(ch, pk, "eCommerce", tipo, dados)
	}

	// Eventos de interesse publicam na exchange Promocoes, no mesmo canal
	// e sob o mesmo lock do publicar acima.
	publicarInteresse = func(routingKey string, dados any) error {
		mu.Lock()
		defer mu.Unlock()
		return publicarEvento(ch, pk, "Promocoes", routingKey, dados)
	}

	pedidos := NovosPedidos()
	avisar := func(msg string) { fmt.Printf("\n[evento] %s\n", msg) }
	notificacoes := NovasConexoes()
	notificar := func(pedido Pedido) {
		data, err := json.Marshal(pedido)
		u.FailOnError(err, "Erro ao serializar o objeto pedido")
		notificacoes.Notificar(pedido.Cliente, string(data))
	}
	servico := NovoServico(pedidos, publicar, avisar, notificar)

	go func() {
		for d := range events {
			var env e.Envelope
			if err := json.Unmarshal(d.Body, &env); err != nil {
				avisar("Mensagem com JSON inválido. Evento descartado.")
				d.Ack(false)
				continue
			}

			produtor := e.ProdutorDoEvento[env.EventType]
			chave := chavesPublicas[produtor]
			if chave == nil || env.Producer != produtor ||
				!s.VerifySignature(chave, string(env.Data), []byte(env.Signature)) {
				avisar(fmt.Sprintf("Assinatura inválida do evento %s. Evento descartado.", env.EventType))
				d.Ack(false)
				continue
			}

			if err := servico.TratarEvento(env); err != nil {
				avisar(fmt.Sprintf("Erro ao tratar %s: %v. Evento descartado.", env.EventType, err))
			}
			d.Ack(false)
		}
	}()

	go func() {
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
			lista := make([]struct {
				ID           string  `json:"id"`
				Availability int     `json:"availability"`
				Nome         string  `json:"nome"`
				Categoria    string  `json:"categoria"`
				Preco        float64 `json:"preco"`
			}, 0, len(produtos))
			for _, produto := range produtos {
				lista = append(lista, struct {
					ID           string  `json:"id"`
					Availability int     `json:"availability"`
					Nome         string  `json:"nome"`
					Categoria    string  `json:"categoria"`
					Preco        float64 `json:"preco"`
				}{
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
				lista := make([]Pedido, 0)
				for _, pedido := range pedidos.Listar() {
					if strings.EqualFold(pedido.Cliente, email) {
						lista = append(lista, pedido)
					}
				}
				return c.JSON(lista)
			}
			return c.JSON(pedidos.Listar())
		})

		app.Post("/pedidos", func(c fiber.Ctx) error {
			var req struct {
				Cliente string `json:"cliente"`
				Itens   []struct {
					ProdutoID  string `json:"produto_id"`
					Quantidade int    `json:"quantidade"`
				} `json:"itens"`
			}
			if err := c.Bind().Body(&req); err != nil || len(req.Itens) == 0 {
				return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"erro": "body inválido ou sem itens"})
			}
			email, ok := normalizarEmail(req.Cliente)
			if !ok {
				return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"erro": "e-mail inválido"})
			}

			itens := make([]e.Item, 0, len(req.Itens))
			for _, item := range req.Itens {
				produto, ok := buscarProdutoCatalogo(produtos, item.ProdutoID)
				if !ok || item.Quantidade <= 0 {
					return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"erro": "body inválido ou sem itens"})
				}
				itens = append(itens, e.Item{
					ProdutoID: item.ProdutoID, Nome: produto.Nome,
					Quantidade: item.Quantidade, PrecoUnitario: produto.Preco,
				})
			}

			pedido, err := servico.CriarPedido(itens, email)
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
			categoria := c.Params("categoria")
			var req struct {
				Email string `json:"email"`
			}
			if err := c.Bind().Body(&req); err != nil || strings.TrimSpace(req.Email) == "" {
				return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"erro": "corpo inválido ou e-mail obrigatório"})
			}
			if err := Registrar(e.InteresseCategoria{Categoria: categoria, Email: strings.TrimSpace(req.Email)}); err != nil {
				return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"erro": "falha ao registrar interesse"})
			}
			return c.SendStatus(fiber.StatusAccepted)
		})

		app.Delete("/interesses/:categoria", func(c fiber.Ctx) error {
			categoria := c.Params("categoria")
			email := strings.TrimSpace(c.Query("email"))
			if email == "" {
				return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"erro": "e-mail obrigatório"})
			}
			if err := Cancelar(e.InteresseCategoria{Categoria: categoria, Email: email}); err != nil {
				return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{"erro": "falha ao cancelar interesse"})
			}
			return c.SendStatus(fiber.StatusAccepted)
		})

		app.Get("/notificacoes", func(c fiber.Ctx) error {
			email, ok := normalizarEmail(c.Query("email"))
			if !ok {
				return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"erro": "e-mail inválido"})
			}
			ch := notificacoes.Abrir(email)
			c.Set(fiber.HeaderContentType, "text/event-stream")
			c.Set(fiber.HeaderCacheControl, "no-cache")
			c.Set(fiber.HeaderConnection, "keep-alive")
			c.RequestCtx().SetBodyStreamWriter(func(w *bufio.Writer) {
				defer notificacoes.Fechar(email, ch)
				for {
					select {
					case msg := <-ch:
						fmt.Fprintf(w, "data: %s\n\n", msg)
					case <-time.After(30 * time.Second):
						fmt.Fprint(w, ": ping\n\n")
					}
					if w.Flush() != nil {
						return
					}
				}
			})
			return nil
		})
		app.Get("/*", static.New("./frontend"))
		log.Fatal(app.Listen(":3001"))
	}()
	NovoMenu(servico, pedidos, produtos, os.Stdin, os.Stdout).Executar()

	fmt.Println("Até mais!")
}

var cliente_estoque = &http.Client{Timeout: 5 * time.Second}

const url_estoque = "http://localhost:3000/list/storage"

type ProdutoEstoque struct {
	ID           string `json:"id"`
	Availability int    `json:"availability"`
}

func listarEstoque() ([]ProdutoEstoque, error) {
	response, err := cliente_estoque.Get(url_estoque)
	if err != nil {
		return nil, fmt.Errorf("listar estoque: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("listar estoque: HTTP %d", response.StatusCode)
	}

	l := []ProdutoEstoque{}
	if err := json.NewDecoder(response.Body).Decode(&l); err != nil {
		return nil, fmt.Errorf("decodificar resposta do estoque: %w", err)
	}
	return l, nil
}

func buscarProdutoCatalogo(produtos []catalogo.Produto, id string) (catalogo.Produto, bool) {
	for _, p := range produtos {
		if strings.EqualFold(p.ID, id) {
			return p, true
		}
	}
	return catalogo.Produto{}, false
}

// publicarInteresse publica eventos de interesse na exchange Promocoes.
// É preenchida no main, que concentra o canal, a chave privada e o lock.
var publicarInteresse func(routingKey string, dados any) error

// Registrar publica na exchange Promocoes, com a routing key
// interesse.promocao, o interesse de um e-mail numa categoria de produtos.
func Registrar(interesse e.InteresseCategoria) error {
	return publicarInteresse(e.InteressePromocao, interesse)
}

// Cancelar publica na exchange Promocoes, com a routing key
// cancelar.interesse, o cancelamento do interesse de um e-mail.
func Cancelar(interesse e.InteresseCategoria) error {
	return publicarInteresse(e.CancelarInteresse, interesse)
}

// publicarEvento monta o envelope, assina o data com a chave do Principal e
// publica na exchange informada, do mesmo jeito que o Estoque e o Pagamento.
// O EventType do envelope é sempre igual à routing key.
func publicarEvento(ch *amqp.Channel, pk *rsa.PrivateKey, exchange, rk string, dados any) error {
	dadosJSON, err := json.Marshal(dados)
	if err != nil {
		return fmt.Errorf("serializar dados de %s: %w", rk, err)
	}

	body := e.Envelope{
		EventID:    uuid.NewString(),
		EventType:  rk,
		Producer:   e.ProdutorDoEvento[rk],
		OccurredAt: time.Now().UTC().Truncate(time.Millisecond),
		Data:       dadosJSON,
		Signature:  base64.StdEncoding.EncodeToString(s.SignPayload(pk, string(dadosJSON))),
	}

	bodyJSON, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("serializar envelope de %s: %w", rk, err)
	}

	err = ch.Publish(
		exchange,
		rk,
		false,
		false,
		amqp.Publishing{
			ContentType:  "application/json",
			DeliveryMode: amqp.Persistent,
			MessageId:    body.EventID,
			Timestamp:    body.OccurredAt,
			Body:         bodyJSON,
		})
	if err != nil {
		return fmt.Errorf("publicar %s: %w", rk, err)
	}
	return nil
}

func normalizarEmail(valor string) (string, bool) {
	email := strings.ToLower(strings.TrimSpace(valor))
	endereco, err := mail.ParseAddress(email)
	return email, err == nil && endereco.Address == email
}
