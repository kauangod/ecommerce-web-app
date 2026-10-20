package mensageria

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/bdsromulo/Trab2-SistDistribuidos/internal/events"
	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"
)

// Usa uma exchange e uma fila exclusivas e temporárias. Não interfere nas
// filas da aplicação nem exige que os microsserviços estejam executando.
func TestRabbitMQPublicacaoConfirmadaEAssinada(t *testing.T) {
	endereco := os.Getenv("RABBITMQ_TEST_URL")
	if endereco == "" {
		t.Skip("defina RABBITMQ_TEST_URL para testar com um broker real")
	}
	conn, err := amqp.Dial(endereco)
	if err != nil {
		t.Fatal("não foi possível conectar ao RabbitMQ de testes")
	}
	defer conn.Close()
	ch, err := conn.Channel()
	if err != nil {
		t.Fatal(err)
	}
	defer ch.Close()
	exchange := "teste.bloco3." + uuid.NewString()
	if err := ch.ExchangeDeclare(exchange, "direct", false, true, false, false, nil); err != nil {
		t.Fatal(err)
	}
	defer ch.ExchangeDelete(exchange, false, false)
	fila, err := ch.QueueDeclare("", false, true, true, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := ch.QueueBind(fila.Name, events.PagamentoAprovado, exchange, false, nil); err != nil {
		t.Fatal(err)
	}
	entregas, err := ch.Consume(fila.Name, "", true, true, false, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	chave, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	pub, err := NovoPublicador(conn, chave, events.Pagamento)
	if err != nil {
		t.Fatal(err)
	}
	defer pub.Close()
	ctx, cancelar := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelar()
	if err := pub.Publicar(ctx, exchange, events.PagamentoAprovado, events.PagamentoAprovadoDados{PedidoID: "42", ValorTotal: 10}); err != nil {
		t.Fatal(err)
	}
	select {
	case d := <-entregas:
		env, err := Validar(d.Body, d.RoutingKey, events.PagamentoAprovado, events.Pagamento, &chave.PublicKey)
		if err != nil {
			t.Fatal(err)
		}
		var dados events.PagamentoAprovadoDados
		if err := json.Unmarshal(env.Data, &dados); err != nil || dados.PedidoID != "42" || dados.ValorTotal != 10 {
			t.Fatalf("contrato inválido: %+v, %v", dados, err)
		}
	case <-ctx.Done():
		t.Fatal("evento não chegou ao consumidor")
	}
}
