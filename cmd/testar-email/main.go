// Envia um e-mail de teste para o vetor configurado em PROMOCOES_EMAILS_TESTE.
// Não depende de RabbitMQ nem cadastra inscrições de promoções.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/bdsromulo/Trab2-SistDistribuidos/internal/execucao"
	"github.com/bdsromulo/Trab2-SistDistribuidos/internal/promocoes"
	"github.com/google/uuid"
)

func main() {
	if err := executar(); err != nil {
		log.Fatal(err)
	}
}

func executar() error {
	if err := execucao.CarregarEnv(".env"); err != nil {
		return err
	}
	config, err := promocoes.CarregarConfigEmail()
	if err != nil {
		return err
	}
	if len(config.DestinatariosTeste) == 0 {
		return fmt.Errorf("adicione ao menos um endereço ao vetor PROMOCOES_EMAILS_TESTE no .env")
	}
	cliente := &promocoes.Resend{APIKey: config.APIKey, Remetente: config.Remetente, HTTP: execucao.ClienteHTTP()}
	idTeste := uuid.NewString()
	var falhas []error
	for i, email := range config.DestinatariosTeste {
		// Evita uma rajada de requisições quando houver vários destinatários.
		if i > 0 {
			time.Sleep(time.Second)
		}
		ctx, cancelar := context.WithTimeout(context.Background(), 15*time.Second)
		err := cliente.EnviarTeste(ctx, idTeste, email)
		cancelar()
		if err != nil {
			falhas = append(falhas, fmt.Errorf("envio para %s: %w", email, err))
			continue
		}
		log.Printf("Resend aceitou o e-mail de teste para %s", email)
	}
	return errors.Join(falhas...)
}
