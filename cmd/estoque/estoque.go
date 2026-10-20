package main

import (
	"encoding/json"
	"log"
	"os"
	"sort"

	u "github.com/bdsromulo/Trab2-SistDistribuidos/internal/utils"
	fiber "github.com/gofiber/fiber/v3"
)

type ProdutoEstoque struct {
	ID           string `json:"id"`
	Availability int    `json:"availability"`
}

type Estoque struct {
	Produtos map[string]*ProdutoEstoque
}

func loadEstoque() Estoque {
	content, err := os.ReadFile("data/estoque.json")
	u.FailOnError(err, "Erro ao carregar o estoque")

	var l []ProdutoEstoque
	err = json.Unmarshal(content, &l)
	u.FailOnError(err, "Erro ao decodificar o estoque")

	e := Estoque{Produtos: make(map[string]*ProdutoEstoque, len(l))}
	for i, p := range l {
		e.Produtos[p.ID] = &l[i]
	}

	return e
}

func saveEstoque(est Estoque) {
	l := make([]ProdutoEstoque, 0, len(est.Produtos))
	for _, p := range est.Produtos {
		l = append(l, *p)
	}
	sort.Slice(l, func(i, j int) bool {
		return l[i].ID < l[j].ID
	})

	data, err := json.MarshalIndent(l, "", "  ")
	u.FailOnError(err, "Erro ao serializar os produtos")

	tmp := "data/estoque.json.tmp"
	err = os.WriteFile(tmp, data, 0o644)
	u.FailOnError(err, "Erro ao salvar o estoque")

	err = os.Rename(tmp, "data/estoque.json")
	u.FailOnError(err, "Erro ao renomear o arquivo do estoque")
}

func imprimirEstoque(est Estoque) {
	ids := make([]string, 0, len(est.Produtos))
	for id := range est.Produtos {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	log.Println("=== Estoque Atual ===")
	for _, id := range ids {
		log.Printf("  Produto %s: %d unidades", id, est.Produtos[id].Availability)
	}
}

func ordenaLista(l []fiber.Map) {
	sort.Slice(l, func(i, j int) bool {
		return l[i]["id"].(string) < l[j]["id"].(string)
	})
}

/*func main() {
	produtos, _ := c.Carregar(c.CaminhoPadrao)
	for _, prod := range produtos {
		log.Printf("%s", prod.ID)
	}
	e := loadEstoque()
	for _, item := range e.Produtos {
		log.Printf("%s: %d", item.ID, e.Produtos[item.ID].Availability)
	}
}*/
