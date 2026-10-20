package promocoes

import (
	"errors"
	"os"
	"testing"

	"github.com/bdsromulo/Trab2-SistDistribuidos/internal/events"
)

func TestEventoDoGatewayAcumulaCategoriasECancelaSomenteSelecionada(t *testing.T) {
	r, caminho := repositorio(t)
	for _, corpo := range []string{
		`{"email":"Pessoa@Example.com","categoria":"a"}`,
		`{"email":"pessoa@example.com","categoria":"B"}`,
		`{"email":"pessoa@example.com","categoria":"A"}`,
	} {
		if err := r.AplicarEventoInteresse(events.InteressePromocao, []byte(corpo)); err != nil {
			t.Fatal(err)
		}
	}
	for _, categoria := range []string{"A", "B"} {
		if got := r.Destinatarios(categoria); len(got) != 1 || got[0] != "pessoa@example.com" {
			t.Fatalf("destinatários %s = %v", categoria, got)
		}
	}
	for i := 0; i < 2; i++ {
		if err := r.AplicarEventoInteresse(events.CancelarInteresse, []byte(`{"email":"pessoa@example.com","categoria":"A"}`)); err != nil {
			t.Fatal(err)
		}
	}
	r, err := AbrirInteresses(caminho, []string{"A", "B", "C"})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Destinatarios("A")) != 0 || len(r.Destinatarios("B")) != 1 {
		t.Fatal("cancelamento de A alterou B ou não persistiu")
	}
}

func TestContratoAnteriorContinuaAceitoPeloAdaptador(t *testing.T) {
	r, _ := repositorio(t)
	for _, corpo := range []string{
		`{"email":"pessoa@example.com","categorias":["A","B"],"acao":"cadastrar"}`,
		`{"email":"pessoa@example.com","categorias":["C"],"acao":"cadastrar"}`,
	} {
		if err := r.AplicarEventoInteresse(events.InteressePromocao, []byte(corpo)); err != nil {
			t.Fatal(err)
		}
	}
	if len(r.Destinatarios("A")) != 0 || len(r.Destinatarios("B")) != 0 || len(r.Destinatarios("C")) != 1 {
		t.Fatal("o contrato anterior deixou de substituir a seleção")
	}
	if err := r.AplicarEventoInteresse(events.InteressePromocao, []byte(`{"email":"pessoa@example.com","acao":"cancelar"}`)); err != nil {
		t.Fatal(err)
	}
	if len(r.Destinatarios("C")) != 0 {
		t.Fatal("cancelamento anterior não removeu a inscrição")
	}
}

func TestEventoDeInteresseInvalidoNaoAlteraInscricao(t *testing.T) {
	r, caminho := repositorio(t)
	atualizar(t, r, "pessoa@example.com", events.InteresseCadastrar, "A")
	antes, err := os.ReadFile(caminho)
	if err != nil {
		t.Fatal(err)
	}
	for _, caso := range []struct{ tipo, corpo string }{
		{events.PedidoCriado, `{"email":"pessoa@example.com","categoria":"B"}`},
		{events.InteressePromocao, `{`},
		{events.InteressePromocao, `{"email":"invalido","categoria":"B"}`},
		{events.InteressePromocao, `{"email":"pessoa@example.com","categoria":"Z"}`},
		{events.CancelarInteresse, `{"email":"pessoa@example.com"}`},
		{events.InteressePromocao, `{"email":"pessoa@example.com","categoria":""}`},
	} {
		if err := r.AplicarEventoInteresse(caso.tipo, []byte(caso.corpo)); !errors.Is(err, ErrInteresseInvalido) {
			t.Fatalf("evento inválido retornou %v: %+v", err, caso)
		}
	}
	depois, err := os.ReadFile(caminho)
	if err != nil {
		t.Fatal(err)
	}
	if string(antes) != string(depois) {
		t.Fatal("evento inválido alterou o arquivo de interesses")
	}
}
