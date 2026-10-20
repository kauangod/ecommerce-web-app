package promocoes

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/bdsromulo/Trab2-SistDistribuidos/internal/events"
	"github.com/bdsromulo/Trab2-SistDistribuidos/internal/execucao"
)

func repositorio(t *testing.T) (*Interesses, string) {
	t.Helper()
	caminho := filepath.Join(t.TempDir(), "interesses.json")
	r, err := AbrirInteresses(caminho, []string{"A", "B", "C"})
	if err != nil {
		t.Fatal(err)
	}
	return r, caminho
}

func atualizar(t *testing.T, r *Interesses, email, acao string, categorias ...string) {
	t.Helper()
	if err := r.Atualizar(events.InteressePromocaoDados{Email: email, Acao: acao, Categorias: categorias}); err != nil {
		t.Fatal(err)
	}
}

func TestInscricaoPersistenciaSubstituicaoECancelamento(t *testing.T) {
	r, caminho := repositorio(t)
	atualizar(t, r, "  Pessoa@Example.com ", events.InteresseCadastrar, "b", " A ", "A")
	atualizar(t, r, "outro@example.com", events.InteresseCadastrar, "C")
	r, err := AbrirInteresses(caminho, []string{"A", "B", "C"})
	if err != nil {
		t.Fatal(err)
	}
	if got := r.Destinatarios("A"); !reflect.DeepEqual(got, []string{"pessoa@example.com"}) {
		t.Fatalf("destinatários A = %v", got)
	}
	atualizar(t, r, "pessoa@example.com", events.InteresseCancelar, "A")
	if len(r.Destinatarios("A")) != 0 || len(r.Destinatarios("B")) != 1 {
		t.Fatal("cancelamento parcial alterou outras categorias")
	}
	atualizar(t, r, "pessoa@example.com", events.InteresseCadastrar, "C")
	if len(r.Destinatarios("B")) != 0 || len(r.Destinatarios("C")) != 2 {
		t.Fatal("cadastro não substituiu a seleção")
	}
	atualizar(t, r, "pessoa@example.com", events.InteresseCancelar)
	atualizar(t, r, "pessoa@example.com", events.InteresseCancelar) // evento repetido
	r, err = AbrirInteresses(caminho, []string{"A", "B", "C"})
	if err != nil {
		t.Fatal(err)
	}
	if got := r.Destinatarios("C"); !reflect.DeepEqual(got, []string{"outro@example.com"}) {
		t.Fatalf("cancelamento total não persistiu: %v", got)
	}
}

func TestInteresseInvalidoNaoAlteraArquivo(t *testing.T) {
	r, caminho := repositorio(t)
	atualizar(t, r, "pessoa@example.com", events.InteresseCadastrar, "A")
	antes, _ := os.ReadFile(caminho)
	for _, dados := range []events.InteressePromocaoDados{
		{Email: "invalido", Acao: events.InteresseCadastrar, Categorias: []string{"A"}},
		{Email: "pessoa@example.com", Acao: events.InteresseCadastrar, Categorias: []string{"Z"}},
		{Email: "pessoa@example.com", Acao: "desconhecida", Categorias: []string{"A"}},
		{Email: "pessoa@example.com", Acao: events.InteresseCadastrar},
	} {
		if err := r.Atualizar(dados); !errors.Is(err, ErrInteresseInvalido) {
			t.Fatalf("interesse aceito: %+v: %v", dados, err)
		}
	}
	depois, _ := os.ReadFile(caminho)
	if string(antes) != string(depois) {
		t.Fatal("arquivo alterado por dados inválidos")
	}
}

func TestFalhaNaPersistenciaNaoMudaInteressesEmMemoria(t *testing.T) {
	r, _ := repositorio(t)
	atualizar(t, r, "pessoa@example.com", events.InteresseCadastrar, "A")
	// Tentar substituir uma pasta por arquivo falha tanto no Windows quanto no Linux.
	r.caminho = t.TempDir()
	err := r.Atualizar(events.InteressePromocaoDados{Email: "pessoa@example.com", Acao: events.InteresseCancelar})
	if err == nil || len(r.Destinatarios("A")) != 1 {
		t.Fatal("falha de persistência alterou a inscrição")
	}
}

type recebimento struct{ destinatario, chave, assunto, texto string }

func TestEmailDeTesteUsaAPISemCadastrarInteresse(t *testing.T) {
	servidor := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		var dados struct {
			To      []string `json:"to"`
			Subject string   `json:"subject"`
			Text    string   `json:"text"`
		}
		if err := json.NewDecoder(req.Body).Decode(&dados); err != nil {
			t.Error(err)
		}
		if req.Header.Get("Authorization") != "Bearer chave-teste" || len(dados.To) != 1 || dados.To[0] != "pessoa@example.com" {
			t.Error("credencial ou destinatário incorreto")
		}
		if !strings.Contains(dados.Subject, "Teste de integração") || strings.Contains(dados.Text, "cadastrou interesse") {
			t.Error("mensagem de teste incorreta")
		}
		_, _ = w.Write([]byte(`{"id":"teste-123"}`))
	}))
	defer servidor.Close()
	c := &Resend{APIKey: "chave-teste", Remetente: "Loja <onboarding@resend.dev>", Endpoint: servidor.URL, HTTP: execucao.ClienteHTTP()}
	if err := c.EnviarTeste(context.Background(), "evento-teste", "pessoa@example.com"); err != nil {
		t.Fatal(err)
	}
}

func TestErroResendExplicaRecusaEOcultaCredencial(t *testing.T) {
	servidor := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"remetente sem domínio verificado; chave-teste"}`))
	}))
	defer servidor.Close()
	c := &Resend{APIKey: "chave-teste", Remetente: "Loja <onboarding@resend.dev>", Endpoint: servidor.URL, HTTP: execucao.ClienteHTTP()}
	err := c.EnviarTeste(context.Background(), "evento-teste", "pessoa@example.com")
	if err == nil || !strings.Contains(err.Error(), "HTTP 403") || !strings.Contains(err.Error(), "domínio verificado") || strings.Contains(err.Error(), "chave-teste") {
		t.Fatalf("erro não explica a recusa ou expõe credencial: %v", err)
	}
}

func TestResendRecebeSomenteCategoriasCadastradas(t *testing.T) {
	r, _ := repositorio(t)
	atualizar(t, r, "a@example.com", events.InteresseCadastrar, "A", "B")
	atualizar(t, r, "b@example.com", events.InteresseCadastrar, "B")
	var mu sync.Mutex
	var recebidos []recebimento
	servidor := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method != "POST" || req.URL.Path != "/emails" || req.Header.Get("Authorization") != "Bearer chave-de-teste" {
			t.Error("contrato REST da Resend inválido")
		}
		var dados struct {
			From    string   `json:"from"`
			To      []string `json:"to"`
			Subject string   `json:"subject"`
			Text    string   `json:"text"`
		}
		if err := json.NewDecoder(req.Body).Decode(&dados); err != nil {
			t.Error(err)
		}
		if len(dados.To) != 1 || dados.From != "Loja <loja@example.com>" {
			t.Error("destinatários ou remetente incorretos")
		}
		mu.Lock()
		recebidos = append(recebidos, recebimento{dados.To[0], req.Header.Get("Idempotency-Key"), dados.Subject, dados.Text})
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"email-teste"}`))
	}))
	defer servidor.Close()
	n := &Notificador{Interesses: r, Email: &Resend{
		APIKey: "chave-de-teste", Remetente: "Loja <loja@example.com>", Endpoint: servidor.URL + "/emails", HTTP: execucao.ClienteHTTP(),
	}}
	promo := events.PromocaoDados{Nome: "Fone", Categoria: "A", PrecoOriginal: 10, PrecoPromocional: 8, Desconto: 20}
	for i := 0; i < 2; i++ {
		if err := n.Notificar(context.Background(), "evento-1", promo); err != nil {
			t.Fatal(err)
		}
	}
	atualizar(t, r, "a@example.com", events.InteresseCancelar)
	if err := n.Notificar(context.Background(), "evento-2", promo); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(recebidos) != 2 || recebidos[0].destinatario != "a@example.com" || recebidos[0].chave == "" || recebidos[0].chave != recebidos[1].chave {
		t.Fatalf("envios ou idempotência incorretos: %+v", recebidos)
	}
	if !strings.Contains(recebidos[0].texto, "R$ 8,00") || !strings.Contains(recebidos[0].assunto, "Fone") {
		t.Fatalf("oferta incompleta: %+v", recebidos[0])
	}
}

type emailComFalha struct{ tentativas map[string]int }

func (e *emailComFalha) Enviar(_ context.Context, _ string, email string, _ events.PromocaoDados) error {
	e.tentativas[email]++
	if email == "b@example.com" && e.tentativas[email] == 1 {
		return errors.New("falha temporária")
	}
	return nil
}

func TestRetryDeEmailNaoRepeteDestinatariosJaConfirmados(t *testing.T) {
	r, _ := repositorio(t)
	atualizar(t, r, "a@example.com", events.InteresseCadastrar, "A")
	atualizar(t, r, "b@example.com", events.InteresseCadastrar, "A")
	e := &emailComFalha{tentativas: make(map[string]int)}
	n := &Notificador{Interesses: r, Email: e}
	promo := events.PromocaoDados{Categoria: "A"}
	if err := n.Notificar(context.Background(), "evento", promo); err == nil {
		t.Fatal("falha deveria retornar ao consumidor")
	}
	if err := n.Notificar(context.Background(), "evento", promo); err != nil {
		t.Fatal(err)
	}
	if e.tentativas["a@example.com"] != 1 || e.tentativas["b@example.com"] != 2 {
		t.Fatalf("reenvio duplicado: %v", e.tentativas)
	}
}
