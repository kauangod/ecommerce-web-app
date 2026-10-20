// Package mensageria mantém o contrato de assinatura do trabalho anterior.
// As declarações de filas e bindings continuam nos comandos de cada serviço.
package mensageria

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/bdsromulo/Trab2-SistDistribuidos/internal/events"
	"github.com/bdsromulo/Trab2-SistDistribuidos/internal/signature"
	"github.com/google/uuid"
	amqp "github.com/rabbitmq/amqp091-go"
)

// Assinar assina somente data, em base64, como os serviços existentes.
func Assinar(chave *rsa.PrivateKey, produtor, tipo string, dados any) (events.Envelope, error) {
	if chave == nil || events.ProdutorDoEvento[tipo] != produtor {
		return events.Envelope{}, errors.New("chave ou produtor inválido")
	}
	bruto, err := json.Marshal(dados)
	if err != nil {
		return events.Envelope{}, err
	}
	return events.Envelope{
		EventID: uuid.NewString(), EventType: tipo, Producer: produtor,
		OccurredAt: time.Now().UTC().Truncate(time.Millisecond), Data: bruto,
		Signature: base64.StdEncoding.EncodeToString(signature.SignPayload(chave, string(bruto))),
	}, nil
}

// Validar também confere o tipo e a routing key: o envelope antigo assina
// apenas data, então os metadados precisam ser conferidos separadamente.
func Validar(bruto []byte, routingKey, tipo, produtor string, chave *rsa.PublicKey) (events.Envelope, error) {
	var env events.Envelope
	if err := json.Unmarshal(bruto, &env); err != nil {
		return env, err
	}
	if chave == nil || env.EventID == "" || routingKey != tipo || env.EventType != tipo ||
		env.Producer != produtor || events.ProdutorDoEvento[tipo] != produtor ||
		!signature.VerifySignature(chave, string(env.Data), []byte(env.Signature)) {
		return env, errors.New("tipo, produtor ou assinatura inválida")
	}
	return env, nil
}

// Publicador usa um canal próprio com confirmação do broker. O mutex evita
// intercalar publicações feitas pelo consumidor e pelo servidor HTTP.
type Publicador struct {
	mu      sync.Mutex
	canal   *amqp.Channel
	chave   *rsa.PrivateKey
	servico string
}

func NovoPublicador(conn *amqp.Connection, chave *rsa.PrivateKey, servico string) (*Publicador, error) {
	ch, err := conn.Channel()
	if err != nil {
		return nil, err
	}
	if err := ch.Confirm(false); err != nil {
		_ = ch.Close()
		return nil, err
	}
	return &Publicador{canal: ch, chave: chave, servico: servico}, nil
}

func (p *Publicador) Close() error { return p.canal.Close() }

func (p *Publicador) Publicar(ctx context.Context, exchange, tipo string, dados any) error {
	env, err := Assinar(p.chave, p.servico, tipo, dados)
	if err != nil {
		return err
	}
	bruto, err := json.Marshal(env)
	if err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	ctx, cancelar := context.WithTimeout(ctx, 5*time.Second)
	defer cancelar()
	confirmacao, err := p.canal.PublishWithDeferredConfirmWithContext(ctx, exchange, tipo, false, false, amqp.Publishing{
		ContentType: "application/json", DeliveryMode: amqp.Persistent,
		MessageId: env.EventID, Timestamp: env.OccurredAt, Body: bruto,
	})
	if err != nil {
		return err
	}
	confirmado, err := confirmacao.WaitContext(ctx)
	if err != nil {
		return err
	}
	if !confirmado {
		return fmt.Errorf("RabbitMQ recusou %s", tipo)
	}
	return nil
}
