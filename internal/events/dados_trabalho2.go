package events

// PagamentoCriadoDados permite ao Gateway repassar o checkout ao frontend
// sem fazer uma chamada direta ao MS Pagamento.
type PagamentoCriadoDados struct {
	PedidoID    string  `json:"pedido_id"`
	CobrancaID  string  `json:"cobranca_id"`
	ValorTotal  float64 `json:"valor_total"`
	CheckoutURL string  `json:"checkout_url"`
}

const (
	InteresseCadastrar = "cadastrar"
	InteresseCancelar  = "cancelar"
)

// InteressePromocaoDados mantém o contrato anterior de interesses para
// mensagens já enfileiradas e clientes existentes. O Gateway atual publica
// InteresseCategoria: uma categoria por evento, sem substituir as demais.
type InteressePromocaoDados struct {
	Email      string   `json:"email"`
	Categorias []string `json:"categorias"`
	Acao       string   `json:"acao"`
}
