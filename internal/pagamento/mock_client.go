package pagamento

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type ClienteMock struct {
	URL   string
	Token string
	HTTP  *http.Client
}

func (c *ClienteMock) Criar(ctx context.Context, dados CriarCobranca) (Cobranca, error) {
	var cobranca Cobranca
	bruto, err := json.Marshal(dados)
	if err != nil {
		return cobranca, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.URL, "/")+"/api/cobrancas", bytes.NewReader(bruto))
	if err != nil {
		return cobranca, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.Token)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return cobranca, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return cobranca, fmt.Errorf("Mock respondeu HTTP %d", resp.StatusCode)
	}
	err = json.NewDecoder(io.LimitReader(resp.Body, 16<<10)).Decode(&cobranca)
	return cobranca, err
}
