package mensageria

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"testing"

	"github.com/bdsromulo/Trab2-SistDistribuidos/internal/events"
	"github.com/bdsromulo/Trab2-SistDistribuidos/internal/signature"
)

func TestAssinaturaMantemCompatibilidadeEDescartaAdulteracao(t *testing.T) {
	chave, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	env, err := Assinar(chave, events.Pagamento, events.PagamentoAprovado, events.PagamentoAprovadoDados{PedidoID: "42", ValorTotal: 10})
	if err != nil {
		t.Fatal(err)
	}
	if !signature.VerifySignature(&chave.PublicKey, string(env.Data), []byte(env.Signature)) {
		t.Fatal("incompatível com o verificador existente da Entrega")
	}
	for _, caso := range []struct {
		nome      string
		adulterar func(*events.Envelope)
		valido    bool
	}{
		{"original", func(*events.Envelope) {}, true},
		{"data alterado", func(e *events.Envelope) { e.Data = json.RawMessage(`{"pedido_id":"outro"}`) }, false},
		{"sem assinatura", func(e *events.Envelope) { e.Signature = "" }, false},
		{"produtor falso", func(e *events.Envelope) { e.Producer = events.Principal }, false},
		{"tipo falso", func(e *events.Envelope) { e.EventType = events.PagamentoRecusado }, false},
		{"sem id", func(e *events.Envelope) { e.EventID = "" }, false},
	} {
		t.Run(caso.nome, func(t *testing.T) {
			copia := env
			caso.adulterar(&copia)
			bruto, _ := json.Marshal(copia)
			_, err := Validar(bruto, events.PagamentoAprovado, events.PagamentoAprovado, events.Pagamento, &chave.PublicKey)
			if (err == nil) != caso.valido {
				t.Fatalf("validação: %v", err)
			}
		})
	}
}
