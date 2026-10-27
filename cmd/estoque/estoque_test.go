package main

import (
	"os"
	"testing"
)

func TestLoadEstoqueUsaOriginal(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.Mkdir("data", 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("data/estoque_original.json", []byte(`[{"id":"P01","availability":7}]`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("data/estoque.json", []byte(`[{"id":"P01","availability":2}]`), 0644); err != nil {
		t.Fatal(err)
	}
	est := loadEstoque()
	if est.Produtos["P01"].Availability != 7 {
		t.Fatalf("estoque: %+v", est.Produtos["P01"])
	}
	saveEstoque(est)
	data, err := os.ReadFile("data/estoque.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 {
		t.Fatal("estoque salvo vazio")
	}
}
