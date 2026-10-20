// MS Pagamento: estoque confirmado -> cobrança REST -> webhook -> evento.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"time"

	"github.com/bdsromulo/Trab2-SistDistribuidos/internal/events"
	"github.com/bdsromulo/Trab2-SistDistribuidos/internal/execucao"
	"github.com/bdsromulo/Trab2-SistDistribuidos/internal/mensageria"
	"github.com/bdsromulo/Trab2-SistDistribuidos/internal/pagamento"
	"github.com/bdsromulo/Trab2-SistDistribuidos/internal/signature"
	amqp "github.com/rabbitmq/amqp091-go"
)

func main() {
	ctx, cancelar := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancelar()
	if err := executar(ctx); err != nil {
		log.Fatal(err)
	}
}

func executar(ctx context.Context) error {
	if err := execucao.CarregarEnv(".env"); err != nil {
		return err
	}
	webhook := execucao.Valor("PAGAMENTO_WEBHOOK_URL", "http://localhost:8082/webhooks/pagamento")
	mockURL := execucao.Valor("MOCK_PUBLIC_URL", "http://localhost:8083")
	if !pagamento.URLValida(webhook) || !pagamento.URLValida(mockURL) {
		return fmt.Errorf("configure URLs HTTP válidas para Pagamento e Mock")
	}
	chave := signature.LoadPrivateKey("cmd/pagamento/keys/private_key.pem")
	if chave == nil {
		return fmt.Errorf("chave privada ausente; execute go run ./cmd/gerar-chaves uma vez")
	}
	publicaEstoque := signature.LoadPublicKey("cmd/pagamento/estoque-pub/public_key.pem")
	conn, err := amqp.Dial(execucao.Valor("RABBITMQ_URL", "amqp://ecommerce:ecommerce@localhost:5672/"))
	if err != nil {
		return err
	}
	defer conn.Close()
	ch, err := conn.Channel()
	if err != nil {
		return err
	}
	defer ch.Close()
	if err := ch.ExchangeDeclare("eCommerce", "direct", true, false, false, false, nil); err != nil {
		return err
	}
	if _, err := ch.QueueDeclare("fila.pagamento", true, false, false, false, nil); err != nil {
		return err
	}
	if err := ch.QueueBind("fila.pagamento", events.PedidoEstoqueOK, "eCommerce", false, nil); err != nil {
		return err
	}
	if err := ch.Qos(1, 0, false); err != nil {
		return err
	}
	publicador, err := mensageria.NovoPublicador(conn, chave, events.Pagamento)
	if err != nil {
		return err
	}
	defer publicador.Close()
	servico := pagamento.NovoServico(&pagamento.ClienteMock{
		URL: mockURL, Token: execucao.Valor("MOCK_API_TOKEN", "desenvolvimento-local"), HTTP: execucao.ClienteHTTP(),
	}, webhook, func(ctx context.Context, tipo string, dados any) error {
		if err := publicador.Publicar(ctx, "eCommerce", tipo, dados); err != nil {
			return err
		}
		log.Printf("Publicado %s: %+v", tipo, dados)
		return nil
	})
	entregas, err := ch.Consume("fila.pagamento", "MS_Pagamento", false, false, false, false, nil)
	if err != nil {
		return err
	}
	erroHTTP := make(chan error, 1)
	go func() {
		erroHTTP <- execucao.Servir(ctx, execucao.Valor("PAGAMENTO_ADDR", "127.0.0.1:8082"), pagamento.Handler(servico))
	}()
	log.Printf("Aguardando estoque; webhook em %s", webhook)
	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-erroHTTP:
			return err
		case d, aberto := <-entregas:
			if !aberto {
				return fmt.Errorf("consumidor de pagamentos desconectado")
			}
			env, err := mensageria.Validar(d.Body, d.RoutingKey, events.PedidoEstoqueOK, events.Estoque, publicaEstoque)
			var dados events.PedidoEstoqueOKDados
			if err == nil {
				err = json.Unmarshal(env.Data, &dados)
			}
			if err != nil {
				log.Printf("Evento de estoque descartado: %v", err)
				if err := d.Ack(false); err != nil {
					return err
				}
				continue
			}
			err = servico.EstoqueConfirmado(ctx, dados)
			if errors.Is(err, pagamento.ErrInvalido) || errors.Is(err, pagamento.ErrConflito) {
				log.Printf("Dados de estoque descartados: %v", err)
			} else if err != nil {
				log.Printf("Cobrança será tentada novamente: %v", err)
				select {
				case <-time.After(2 * time.Second):
				case <-ctx.Done():
					return nil
				}
				if err := d.Nack(false, true); err != nil {
					return err
				}
				continue
			}
			if err := d.Ack(false); err != nil {
				return err
			}
		}
	}
}
