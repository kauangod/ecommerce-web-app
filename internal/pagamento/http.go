package pagamento

import (
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/bdsromulo/Trab2-SistDistribuidos/internal/httpjson"
)

func Handler(servico *Servico) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /webhooks/pagamento", func(w http.ResponseWriter, r *http.Request) {
		var resultado Resultado
		if err := httpjson.Ler(w, r, &resultado); err != nil {
			httpjson.Erro(w, http.StatusBadRequest, "JSON inválido")
			return
		}
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			httpjson.Erro(w, http.StatusUnauthorized, "webhook não autorizado")
			return
		}
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		err := servico.ReceberResultado(r.Context(), resultado, token)
		switch {
		case err == nil:
			httpjson.Escrever(w, http.StatusOK, map[string]string{"status": "recebido"})
		case errors.Is(err, ErrInvalido):
			httpjson.Erro(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, ErrNaoAutorizado):
			httpjson.Erro(w, http.StatusUnauthorized, err.Error())
		case errors.Is(err, ErrNaoEncontrado):
			httpjson.Erro(w, http.StatusNotFound, err.Error())
		case errors.Is(err, ErrConflito):
			httpjson.Erro(w, http.StatusConflict, err.Error())
		default:
			log.Printf("Falha ao publicar resultado do pagamento: %v", err)
			httpjson.Erro(w, http.StatusServiceUnavailable, "não foi possível registrar o resultado; tente novamente")
		}
	})
	return mux
}
