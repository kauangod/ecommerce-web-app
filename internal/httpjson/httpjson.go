// Package httpjson reúne os limites e respostas dos dois servidores HTTP.
package httpjson

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

// Ler aceita exatamente um objeto JSON, limitado a 16 KiB.
func Ler(w http.ResponseWriter, r *http.Request, destino any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	defer r.Body.Close()
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(destino); err != nil {
		return err
	}
	if err := dec.Decode(new(any)); !errors.Is(err, io.EOF) {
		return errors.New("envie apenas um objeto JSON")
	}
	return nil
}

func Escrever(w http.ResponseWriter, status int, dados any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(dados)
}

func Erro(w http.ResponseWriter, status int, mensagem string) {
	Escrever(w, status, map[string]string{"erro": mensagem})
}
