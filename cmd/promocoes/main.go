// Promoções recebe interesses, publica ofertas e envia e-mails por categoria.
package main

import (
	"context"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"math/rand/v2"
	"os"
	"os/signal"
	"time"

	"github.com/bdsromulo/Trab2-SistDistribuidos/internal/catalogo"
	"github.com/bdsromulo/Trab2-SistDistribuidos/internal/events"
	"github.com/bdsromulo/Trab2-SistDistribuidos/internal/execucao"
	"github.com/bdsromulo/Trab2-SistDistribuidos/internal/mensageria"
	"github.com/bdsromulo/Trab2-SistDistribuidos/internal/promocoes"
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
	configEmail, err := promocoes.CarregarConfigEmail()
	if err != nil {
		return err
	}
	var intervalo time.Duration
	if valor := os.Getenv("PROMOCOES_INTERVALO"); valor != "" {
		var err error
		intervalo, err = time.ParseDuration(valor)
		if err != nil || intervalo < time.Second {
			return fmt.Errorf("PROMOCOES_INTERVALO deve ser uma duração de pelo menos 1s")
		}
	}
	produtos, err := catalogo.Carregar(catalogo.CaminhoPadrao)
	if err != nil {
		return err
	}
	if len(produtos) == 0 {
		return fmt.Errorf("catálogo vazio")
	}
	categorias := make([]string, 0)
	for _, p := range produtos {
		categorias = append(categorias, p.Categoria)
	}
	interesses, err := promocoes.AbrirInteresses(execucao.Valor("PROMOCOES_INTERESSES_PATH", "data/interesses.local.json"), categorias)
	if err != nil {
		return err
	}
	chave := signature.LoadPrivateKey("cmd/promocoes/keys/private_key.pem")
	if chave == nil {
		return fmt.Errorf("chave privada ausente; execute go run ./cmd/gerar-chaves uma vez")
	}
	publicaPrincipal := signature.LoadPublicKey("cmd/promocoes/principal-pub/public_key.pem")
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
	if err := ch.ExchangeDeclare("Promocoes", "topic", true, false, false, false, nil); err != nil {
		return err
	}
	if _, err := ch.QueueDeclare("fila.promocoes.interesses", true, false, false, false, nil); err != nil {
		return err
	}
	for _, tipo := range []string{events.InteressePromocao, events.CancelarInteresse} {
		if err := ch.QueueBind("fila.promocoes.interesses", tipo, "Promocoes", false, nil); err != nil {
			return err
		}
	}
	// Preserva o binding do contrato anterior para mensagens e clientes locais.
	if err := ch.QueueBind("fila.promocoes.interesses", events.InteressePromocao, "eCommerce", false, nil); err != nil {
		return err
	}
	if err := ch.Qos(1, 0, false); err != nil {
		return err
	}
	entregas, err := ch.Consume("fila.promocoes.interesses", "MS_Promocoes_Interesses", false, false, false, false, nil)
	if err != nil {
		return err
	}
	publicador, err := mensageria.NovoPublicador(conn, chave, events.Promocoes)
	if err != nil {
		return err
	}
	defer publicador.Close()

	// Uma fila própria guarda as ofertas enquanto a API externa está fora do
	// ar. C1 e C2 continuam recebendo as mesmas ofertas em suas próprias filas.
	chEmails, err := conn.Channel()
	if err != nil {
		return err
	}
	defer chEmails.Close()
	if _, err := chEmails.QueueDeclare("fila.promocoes.emails", true, false, false, false, nil); err != nil {
		return err
	}
	if err := chEmails.QueueBind("fila.promocoes.emails", events.PrefixoPromocao+"*", "Promocoes", false, nil); err != nil {
		return err
	}
	if err := chEmails.Qos(1, 0, false); err != nil {
		return err
	}
	emails, err := chEmails.Consume("fila.promocoes.emails", "MS_Promocoes_Emails", false, false, false, false, nil)
	if err != nil {
		return err
	}
	notificador := &promocoes.Notificador{Interesses: interesses, Email: &promocoes.Resend{
		APIKey: configEmail.APIKey, Remetente: configEmail.Remetente, HTTP: execucao.ClienteHTTP(),
	}}
	erroEmails := make(chan error, 1)
	go func() { erroEmails <- consumirEmails(ctx, emails, &chave.PublicKey, notificador) }()
	timer := time.NewTimer(0)
	defer timer.Stop()
	log.Printf("Aguardando interesses; ofertas a cada 20 a 30 s (ou PROMOCOES_INTERVALO)")
	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-erroEmails:
			return err
		case <-timer.C:
			p := produtos[rand.IntN(len(produtos))]
			desconto := 5 + rand.IntN(46)
			promo := events.PromocaoDados{
				ProdutoID: p.ID, Nome: p.Nome, Categoria: p.Categoria, PrecoOriginal: p.Preco,
				Desconto: desconto, PrecoPromocional: math.Round(p.Preco*float64(100-desconto)) / 100,
			}
			if err := publicador.Publicar(ctx, "Promocoes", events.TipoPromocao(p.Categoria), promo); err != nil {
				return err
			}
			log.Printf("Publicado %s: %s (-%d%%)", events.TipoPromocao(p.Categoria), p.Nome, desconto)
			duracao := intervalo
			if duracao == 0 {
				duracao = time.Duration(20+rand.IntN(11)) * time.Second
			}
			timer.Reset(duracao)
		case d, aberto := <-entregas:
			if !aberto {
				return fmt.Errorf("consumidor de interesses desconectado")
			}
			env, err := mensageria.Validar(d.Body, d.RoutingKey, d.RoutingKey, events.Principal, publicaPrincipal)
			if d.RoutingKey != events.InteressePromocao && d.RoutingKey != events.CancelarInteresse {
				err = fmt.Errorf("tipo de interesse desconhecido")
			}
			if err != nil {
				log.Printf("Interesse descartado: %v", err)
			} else if err := interesses.AplicarEventoInteresse(env.EventType, env.Data); err != nil {
				if errors.Is(err, promocoes.ErrInteresseInvalido) {
					log.Printf("Interesse descartado: %v", err)
				} else {
					log.Printf("Falha ao salvar interesse: %v", err)
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
			} else {
				log.Printf("Interesse atualizado (%s)", env.EventType)
			}
			if err := d.Ack(false); err != nil {
				return err
			}
		}
	}
}

func consumirEmails(ctx context.Context, entregas <-chan amqp.Delivery, chave *rsa.PublicKey, n *promocoes.Notificador) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		case d, aberto := <-entregas:
			if !aberto {
				return fmt.Errorf("consumidor de e-mails desconectado")
			}
			env, err := mensageria.Validar(d.Body, d.RoutingKey, d.RoutingKey, events.Promocoes, chave)
			var promo events.PromocaoDados
			if err == nil {
				err = json.Unmarshal(env.Data, &promo)
			}
			if err != nil || events.TipoPromocao(promo.Categoria) != d.RoutingKey {
				log.Printf("Promoção inválida descartada")
			} else if err := n.Notificar(ctx, env.EventID, promo); err != nil {
				log.Printf("E-mail será tentado novamente: %v", err)
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
