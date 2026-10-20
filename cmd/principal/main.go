// Microsserviço Principal: menu no terminal, pedidos e status.
//
// Duas goroutines trabalham juntas: a principal lê o menu no terminal e
// outra consome a fila.principal, atualizando o status dos pedidos.
package main

import (
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
	"bufio"

	"github.com/bdsromulo/Trab2-SistDistribuidos/internal/catalogo"
	e "github.com/bdsromulo/Trab2-SistDistribuidos/internal/events"
	s "github.com/bdsromulo/Trab2-SistDistribuidos/internal/signature"
	u "github.com/bdsromulo/Trab2-SistDistribuidos/internal/utils"
	fiber "github.com/gofiber/fiber/v3"
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

	for _, rk := range []string{e.PedidoEstoqueOK, e.EstoqueIndisponivel, e.PagamentoAprovado, e.PagamentoRecusado, e.PedidoEnviado} {
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
	notificar_cliente := func(pedido Pedido){
		data, err := json.Marshal(pedido)
		u.FailOnError(err, "Erro ao serializar o objeto pedido")
		notificacoes.Notificar(pedido.Cliente, string(data))
	}

	servico := NovoServico(pedidos, publicar, avisar, notificar_cliente)

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
			l := listarEstoque()
			return c.JSON(l)
		})

		app.Get("/notificacoes", func(c fiber.Ctx) error{
			email := c.Query("email")
			if email == "" {
				return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
					"erro": "e-mail obrigatório"})
			}

			ch := notificacoes.Abrir(email)
			c.Set(fiber.HeaderContentType, "text/event-stream")
			c.Set(fiber.HeaderCacheControl, "no-cache")
			c.Set(fiber.HeaderConnection, "keep-alive")

			c.RequestCtx().SetBodyStreamWriter(func(w *bufio.Writer){
				defer notificacoes.Fechar(email, ch)
				for {
					select {
					case msg := <-ch:
						fmt.Fprintf(w, "data: %s\n\n", msg)
					case <-time.After(30 * time.Second):
						fmt.Fprintf(w, ": ping\n\n")
					}
					if w.Flush() != nil {
						return
					}
				}
			})
			return nil
		})

		app.Delete("/pedidos/:id", func(c fiber.Ctx) error {
			err := servico.ExcluirPeloUsuario(c.Params("id"))
			u.FailOnError(err, "Erro ao excluir o pedido")
			return c.SendStatus(fiber.StatusAccepted)
		})

		app.Put("/interesses/:categoria", func(c fiber.Ctx) error {
			req := struct {
				Email string `json:"email"`
			}{}
			err := c.Bind().Body(&req)
			u.FailOnError(err, "Erro")
			email := req.Email
			if email == "" {
				return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
					"erro": "e-mail obrigatório"})
			}
			interesse := e.InteresseCategoria{
				Categoria: c.Params("categoria"),
				Email:     email,
			}
			if err := Registrar(interesse); err != nil {
				return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
					"erro": "falha ao registrar interesse"})
			}
			return c.SendStatus(fiber.StatusAccepted)
		})

		app.Delete("/interesses/:categoria", func(c fiber.Ctx) error {
			email := c.Query("email")
			interesse := e.InteresseCategoria{
				Categoria: c.Params("categoria"),
				Email:     email,
			}
			if email == "" {
				return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
					"erro": "e-mail obrigatório"})
			}
			if err := Cancelar(interesse); err != nil {
				return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
					"erro": "falha ao cancelar interesse"})
			}
			return c.SendStatus(fiber.StatusAccepted)
		})

		app.Post("/pedidos", func(c fiber.Ctx) error {
			req := struct{
				Cliente string `json:"cliente"`
				Itens []struct{
					ProdutoID string `json:"produto_id"`
					Quantidade int `json:"quantidade"`
				} `json:"itens"`
			}{}
			err := c.Bind().Body(&req)
			u.FailOnError(err, "Erro")

			if req.Cliente == "" {
				return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
					"erro": "cliente (e-mail) obrigatório"})
			}

			items := []e.Item{}
			for _, i := range req.Itens{
				prod, ok := buscarProdutoCatalogo(produtos, i.ProdutoID)
				if !ok || i.Quantidade <= 0{
					return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"erro": "body inválido ou sem itens"})
				}
				items = append(items, e.Item{
						ProdutoID: i.ProdutoID,
						Nome: prod.Nome,
						Quantidade: i.Quantidade,
						PrecoUnitario: prod.Preco,
				})
			}
			pedido, err := servico.CriarPedido(items, req.Cliente)
			if err != nil {
				return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"erro": err.Error()})
			}

			return c.Status(fiber.StatusCreated).JSON(pedido)
		})

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

func listarEstoque() []ProdutoEstoque {
	response, err := cliente_estoque.Get(url_estoque)
	u.FailOnError(err, "Erro ao listar estoque")
	defer response.Body.Close()

	l := []ProdutoEstoque{}
	err = json.NewDecoder(response.Body).Decode(&l)
	u.FailOnError(err, "Erro ao decodificar resposta do estoque")
	return l
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
