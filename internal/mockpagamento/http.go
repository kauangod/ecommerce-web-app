package mockpagamento

import (
	"crypto/subtle"
	_ "embed"
	"errors"
	"log"
	"net/http"

	"github.com/bdsromulo/Trab2-SistDistribuidos/internal/httpjson"
	"github.com/bdsromulo/Trab2-SistDistribuidos/internal/pagamento"
)

//go:embed checkout.html
var checkout []byte

func Handler(servico *Servico, tokenAPI string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/cobrancas", func(w http.ResponseWriter, r *http.Request) {
		if tokenAPI == "" || subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+tokenAPI)) != 1 {
			httpjson.Erro(w, http.StatusUnauthorized, "credencial inválida")
			return
		}
		var dados pagamento.CriarCobranca
		if err := httpjson.Ler(w, r, &dados); err != nil {
			httpjson.Erro(w, http.StatusBadRequest, "JSON inválido")
			return
		}
		c, err := servico.Criar(dados)
		if err != nil {
			responderErro(w, err)
			return
		}
		w.Header().Set("Location", c.CheckoutURL)
		httpjson.Escrever(w, http.StatusCreated, c)
	})
	mux.HandleFunc("GET /api/cobrancas/{id}", func(w http.ResponseWriter, r *http.Request) {
		c, err := servico.Buscar(r.PathValue("id"))
		if err != nil {
			responderErro(w, err)
			return
		}
		httpjson.Escrever(w, http.StatusOK, c)
	})
	mux.HandleFunc("POST /api/cobrancas/{id}/resultado", func(w http.ResponseWriter, r *http.Request) {
		// A página e a API são servidas pela mesma origem.
		if origem := r.Header.Get("Origin"); origem != "" && origem != servico.URLPublica {
			httpjson.Erro(w, http.StatusForbidden, "origem inválida")
			return
		}
		var dados struct {
			Status string `json:"status"`
		}
		if err := httpjson.Ler(w, r, &dados); err != nil {
			httpjson.Erro(w, http.StatusBadRequest, "JSON inválido")
			return
		}
		c, err := servico.Escolher(r.Context(), r.PathValue("id"), dados.Status)
		if err != nil {
			responderErro(w, err)
			return
		}
		httpjson.Escrever(w, http.StatusOK, c)
	})
	mux.HandleFunc("GET /checkout/{id}", func(w http.ResponseWriter, r *http.Request) {
		if _, err := servico.Buscar(r.PathValue("id")); err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		_, _ = w.Write(checkout)
	})
	return mux
}

func responderErro(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, pagamento.ErrInvalido):
		httpjson.Erro(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, pagamento.ErrNaoEncontrado):
		httpjson.Erro(w, http.StatusNotFound, err.Error())
	case errors.Is(err, pagamento.ErrConflito):
		httpjson.Erro(w, http.StatusConflict, err.Error())
	default:
		log.Printf("Falha no webhook: %v", err)
		httpjson.Erro(w, http.StatusBadGateway, "não foi possível confirmar o resultado; tente novamente")
	}
}
