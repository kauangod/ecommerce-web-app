// Package promocoes mantém inscrições e envia ofertas por categoria.
package promocoes

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/mail"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/bdsromulo/Trab2-SistDistribuidos/internal/events"
)

var ErrInteresseInvalido = errors.New("interesse inválido")

// Interesses é um repositório JSON local. A escrita é feita em arquivo
// temporário antes de substituir o anterior e de alterar o estado em memória.
type Interesses struct {
	mu         sync.Mutex
	caminho    string
	porEmail   map[string][]string
	categorias map[string]bool
}

func AbrirInteresses(caminho string, categorias []string) (*Interesses, error) {
	r := &Interesses{caminho: caminho, porEmail: make(map[string][]string), categorias: make(map[string]bool)}
	for _, c := range categorias {
		r.categorias[c] = true
	}
	bruto, err := os.ReadFile(caminho)
	if errors.Is(err, os.ErrNotExist) {
		return r, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(bruto, &r.porEmail); err != nil {
		return nil, fmt.Errorf("ler interesses: %w", err)
	}
	if r.porEmail == nil {
		return nil, errors.New("arquivo de interesses deve conter um objeto JSON")
	}
	for email, cats := range r.porEmail {
		if _, err := r.normalizar(events.InteressePromocaoDados{Email: email, Categorias: cats, Acao: events.InteresseCadastrar}); err != nil {
			return nil, fmt.Errorf("arquivo de interesses: %w", err)
		}
	}
	return r, nil
}

func (r *Interesses) normalizar(d events.InteressePromocaoDados) (events.InteressePromocaoDados, error) {
	d.Email = strings.ToLower(strings.TrimSpace(d.Email))
	endereco, err := mail.ParseAddress(d.Email)
	if err != nil || endereco.Address != d.Email || (d.Acao != events.InteresseCadastrar && d.Acao != events.InteresseCancelar) {
		return d, ErrInteresseInvalido
	}
	if d.Acao == events.InteresseCadastrar && len(d.Categorias) == 0 {
		return d, fmt.Errorf("%w: selecione ao menos uma categoria", ErrInteresseInvalido)
	}
	d.Categorias = append([]string(nil), d.Categorias...)
	for i, c := range d.Categorias {
		c = strings.ToUpper(strings.TrimSpace(c))
		if !r.categorias[c] {
			return d, fmt.Errorf("%w: categoria desconhecida", ErrInteresseInvalido)
		}
		d.Categorias[i] = c
	}
	sort.Strings(d.Categorias)
	d.Categorias = slices.Compact(d.Categorias)
	return d, nil
}

func (r *Interesses) Atualizar(d events.InteressePromocaoDados) error {
	return r.atualizar(d, false)
}

// RegistrarCategoria acrescenta a categoria enviada pelo Gateway à seleção
// existente. A atualização e a persistência acontecem sob o mesmo mutex.
func (r *Interesses) RegistrarCategoria(d events.InteresseCategoria) error {
	return r.atualizar(events.InteressePromocaoDados{
		Email: d.Email, Categorias: []string{d.Categoria}, Acao: events.InteresseCadastrar,
	}, true)
}

func (r *Interesses) atualizar(d events.InteressePromocaoDados, acrescentar bool) error {
	d, err := r.normalizar(d)
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	proximo := make(map[string][]string, len(r.porEmail)+1)
	for email, categorias := range r.porEmail {
		proximo[email] = append([]string(nil), categorias...)
	}
	if d.Acao == events.InteresseCadastrar {
		if acrescentar {
			categorias := append(proximo[d.Email], d.Categorias...)
			sort.Strings(categorias)
			proximo[d.Email] = slices.Compact(categorias)
		} else {
			proximo[d.Email] = d.Categorias
		}
	} else if len(d.Categorias) == 0 {
		delete(proximo, d.Email)
	} else {
		mantidas := make([]string, 0)
		for _, c := range proximo[d.Email] {
			if !slices.Contains(d.Categorias, c) {
				mantidas = append(mantidas, c)
			}
		}
		if len(mantidas) == 0 {
			delete(proximo, d.Email)
		} else {
			proximo[d.Email] = mantidas
		}
	}
	bruto, err := json.MarshalIndent(proximo, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(r.caminho), 0700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(r.caminho), "interesses-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	_, gravarErr := tmp.Write(bruto)
	fecharErr := tmp.Close()
	if gravarErr != nil {
		return gravarErr
	}
	if fecharErr != nil {
		return fecharErr
	}
	if err := os.Rename(tmp.Name(), r.caminho); err != nil {
		return err
	}
	r.porEmail = proximo
	return nil
}

func (r *Interesses) Destinatarios(categoria string) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	emails := make([]string, 0)
	for email, categorias := range r.porEmail {
		if slices.Contains(categorias, categoria) {
			emails = append(emails, email)
		}
	}
	sort.Strings(emails)
	return emails
}
