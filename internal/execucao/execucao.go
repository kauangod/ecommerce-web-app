// Package execucao contém apenas configuração e ciclo de vida dos novos processos.
package execucao

import (
	"context"
	"errors"
	"net/http"
	"os"
	"time"
)

func Valor(nome, padrao string) string {
	if valor := os.Getenv(nome); valor != "" {
		return valor
	}
	return padrao
}

func ClienteHTTP() *http.Client {
	return &http.Client{
		Timeout:       10 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// Servir encerra conexões com um prazo, inclusive quando o usuário usa Ctrl+C.
func Servir(ctx context.Context, endereco string, handler http.Handler) error {
	servidor := &http.Server{
		Addr: endereco, Handler: handler, ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout: 15 * time.Second, WriteTimeout: 20 * time.Second, IdleTimeout: 60 * time.Second,
	}
	fim := make(chan struct{})
	defer close(fim)
	go func() {
		select {
		case <-ctx.Done():
			prazo, cancelar := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancelar()
			_ = servidor.Shutdown(prazo)
		case <-fim:
		}
	}()
	err := servidor.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
