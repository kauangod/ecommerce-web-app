package execucao

import (
	"errors"
	"fmt"
	"os"

	"github.com/joho/godotenv"
)

// CarregarEnv lê a configuração local sem substituir variáveis já definidas
// no terminal. A ausência do arquivo é permitida para execução em produção.
func CarregarEnv(caminho string) error {
	err := godotenv.Load(caminho)
	if err == nil || errors.Is(err, os.ErrNotExist) {
		return nil
	}
	// O erro do parser pode conter uma linha com credenciais; não a exibimos.
	return fmt.Errorf("não foi possível carregar %s; confira o formato e a permissão de leitura", caminho)
}
