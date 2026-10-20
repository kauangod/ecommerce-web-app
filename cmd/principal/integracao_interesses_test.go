package main

import (
	"crypto/rand"
	"crypto/rsa"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/bdsromulo/Trab2-SistDistribuidos/internal/events"
	"github.com/bdsromulo/Trab2-SistDistribuidos/internal/mensageria"
	"github.com/bdsromulo/Trab2-SistDistribuidos/internal/promocoes"
	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"
)

// Exercita as funções usadas pelos endpoints do Gateway, a publicação
// assinada existente e o adaptador de Promoções com um RabbitMQ real.
// A exchange e a fila são temporárias e exclusivas deste teste.
func TestGatewayRabbitMQPromocoes(t *testing.T) {
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
	exchange := "teste.integracao.interesses." + uuid.NewString()
	if err := ch.ExchangeDeclare(exchange, "topic", false, true, false, false, nil); err != nil {
		t.Fatal(err)
	}
	defer ch.ExchangeDelete(exchange, false, false)
	fila, err := ch.QueueDeclare("", false, true, true, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tipo := range []string{events.InteressePromocao, events.CancelarInteresse} {
		if err := ch.QueueBind(fila.Name, tipo, exchange, false, nil); err != nil {
			t.Fatal(err)
		}
	}
	entregas, err := ch.Consume(fila.Name, "", true, true, false, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	chave, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	anterior := publicarInteresse
	publicarInteresse = func(tipo string, dados any) error {
		return publicarEvento(ch, chave, exchange, tipo, dados)
	}
	defer func() { publicarInteresse = anterior }()
	caminho := filepath.Join(t.TempDir(), "interesses.json")
	r, err := promocoes.AbrirInteresses(caminho, []string{"A", "B", "C"})
	if err != nil {
		t.Fatal(err)
	}
	for _, passo := range []struct {
		tipo, categoria  string
		esperaA, esperaB int
	}{
		{events.InteressePromocao, "A", 1, 0},
		{events.InteressePromocao, "B", 1, 1},
		{events.InteressePromocao, "A", 1, 1},
		{events.CancelarInteresse, "A", 0, 1},
		{events.CancelarInteresse, "A", 0, 1},
	} {
		interesse := events.InteresseCategoria{Email: "pessoa@example.com", Categoria: passo.categoria}
		var err error
		if passo.tipo == events.InteressePromocao {
			err = Registrar(interesse)
		} else {
			err = Cancelar(interesse)
		}
		if err != nil {
			t.Fatal(err)
		}
		select {
		case d, aberto := <-entregas:
			if !aberto {
				t.Fatal("consumidor de testes desconectado")
			}
			env, err := mensageria.Validar(d.Body, d.RoutingKey, passo.tipo, events.Principal, &chave.PublicKey)
			if err != nil {
				t.Fatal(err)
			}
			if err := r.AplicarEventoInteresse(env.EventType, env.Data); err != nil {
				t.Fatal(err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("interesse não chegou pelo RabbitMQ")
		}
		if len(r.Destinatarios("A")) != passo.esperaA || len(r.Destinatarios("B")) != passo.esperaB {
			t.Fatalf("inscrições inesperadas depois de %+v", passo)
		}
	}
	r, err = promocoes.AbrirInteresses(caminho, []string{"A", "B", "C"})
	if err != nil || len(r.Destinatarios("A")) != 0 || len(r.Destinatarios("B")) != 1 {
		t.Fatalf("interesses não persistiram: %v", err)
	}
}
