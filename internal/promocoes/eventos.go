package promocoes

import (
	"encoding/json"
	"fmt"

	"github.com/bdsromulo/Trab2-SistDistribuidos/internal/events"
)

// AplicarEventoInteresse recebe os dados depois da verificação da assinatura.
// O Gateway atual envia uma categoria por evento. O contrato anterior continua
// aceito para processar mensagens que já estavam na fila antes da integração.
func (r *Interesses) AplicarEventoInteresse(tipo string, bruto []byte) error {
	if tipo != events.InteressePromocao && tipo != events.CancelarInteresse {
		return fmt.Errorf("%w: tipo de evento desconhecido", ErrInteresseInvalido)
	}
	var interesse events.InteresseCategoria
	if err := json.Unmarshal(bruto, &interesse); err != nil {
		return fmt.Errorf("%w: JSON inválido", ErrInteresseInvalido)
	}
	if interesse.Categoria != "" {
		if tipo == events.InteressePromocao {
			return r.RegistrarCategoria(interesse)
		}
		return r.Atualizar(events.InteressePromocaoDados{
			Email: interesse.Email, Categorias: []string{interesse.Categoria}, Acao: events.InteresseCancelar,
		})
	}

	if tipo == events.CancelarInteresse {
		return fmt.Errorf("%w: categoria obrigatória", ErrInteresseInvalido)
	}
	var anterior events.InteressePromocaoDados
	if err := json.Unmarshal(bruto, &anterior); err != nil {
		return fmt.Errorf("%w: JSON inválido", ErrInteresseInvalido)
	}
	return r.Atualizar(anterior)
}
