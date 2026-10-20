// Mock externo: REST para criar cobranças e checkout HTML/JavaScript.
package main

import (
	"context"
	"log"
	"os"
	"os/signal"

	"github.com/bdsromulo/Trab2-SistDistribuidos/internal/execucao"
	"github.com/bdsromulo/Trab2-SistDistribuidos/internal/mockpagamento"
	"github.com/bdsromulo/Trab2-SistDistribuidos/internal/pagamento"
)

func main() {
	if err := execucao.CarregarEnv(".env"); err != nil {
		log.Fatal(err)
	}
	ctx, cancelar := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancelar()
	urlPublica := execucao.Valor("MOCK_PUBLIC_URL", "http://localhost:8083")
	webhook := execucao.Valor("PAGAMENTO_WEBHOOK_URL", "http://localhost:8082/webhooks/pagamento")
	if !pagamento.URLValida(urlPublica) || !pagamento.URLValida(webhook) {
		log.Fatal("MOCK_PUBLIC_URL e PAGAMENTO_WEBHOOK_URL devem ser URLs HTTP válidas")
	}
	servico := mockpagamento.NovoServico(urlPublica, webhook, execucao.ClienteHTTP())
	log.Printf("Mock disponível em %s; aguardando cobranças", urlPublica)
	err := execucao.Servir(ctx, execucao.Valor("MOCK_ADDR", "127.0.0.1:8083"),
		mockpagamento.Handler(servico, execucao.Valor("MOCK_API_TOKEN", "desenvolvimento-local")))
	if err != nil {
		log.Fatal(err)
	}
}
