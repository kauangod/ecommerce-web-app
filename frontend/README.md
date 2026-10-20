

## Modo de demonstração

Abra `http://localhost:<porta>/?demo=1` para demonstrar a interface sem o
backend. Nesse modo, catálogo, pedidos e interesses em promoções usam apenas o
armazenamento local do navegador. Sem `?demo=1`, a interface usa os endpoints
REST e SSE reais descritos abaixo.

## Contrato esperado do API Gateway

Todos os erros REST devem usar um status HTTP adequado e, preferencialmente,
um JSON como `{ "erro": "mensagem para o usuário" }`.

### Catálogo

`GET /api/produtos`

Resposta: um array ou `{ "produtos": [...] }`. Cada produto segue o catálogo
atual e pode acrescentar a quantidade do MS Estoque:

```json
{
  "id": "P01",
  "nome": "Fone de ouvido Bluetooth",
  "categoria": "A",
  "preco": 199.90,
  "quantidade": 8,
  "disponivel": true
}
```

### Pedidos

`POST /api/pedidos`

```json
{
  "itens": [
    { "produto_id": "P01", "quantidade": 2 }
  ]
}
```

Resposta `201 Created`: o pedido abaixo. `checkout_url` é opcional na criação;
se a cobrança só existir depois de `pedido.estoque_ok`, a URL deve chegar em uma
notificação SSE posterior.

`GET /api/pedidos` retorna um array ou `{ "pedidos": [...] }`.

`DELETE /api/pedidos/{id}` cancela o pedido e pode responder `204 No Content`.

```json
{
  "id": "a1b2c3d4",
  "itens": [
    {
      "produto_id": "P01",
      "nome": "Fone de ouvido Bluetooth",
      "quantidade": 2,
      "preco_unitario": 199.90
    }
  ],
  "valor_total": 399.80,
  "status": "aguardando confirmação",
  "motivo": "",
  "detalhe": "",
  "criado_em": "2026-10-19T20:30:00Z",
  "checkout_url": "https://mock-pagamento.test/checkout/abc"
}
```

Os estados exibidos seguem os existentes em `cmd/principal/pedidos.go`:

- `aguardando confirmação`;
- `aguardando estoque`;
- `aguardando pagamento`;
- `pagamento aprovado`;
- `enviado`;
- `excluído`.

### Interesses em promoções

`POST /api/interesses-promocao`

```json
{
  "email": "cliente@exemplo.com",
  "categorias": ["A", "C"]
}
```

`DELETE /api/interesses-promocao` recebe o mesmo formato. Se `categorias` for
um array vazio, deve cancelar todos os interesses do e-mail.

### Atualizações SSE

`GET /api/eventos` deve responder com `Content-Type: text/event-stream`, manter a
conexão aberta e enviar uma linha `id:` para permitir reconexão com
`Last-Event-ID`. O frontend aceita eventos nomeados ou mensagens genéricas.

Exemplo nomeado:

```text
id: 40c665bb
event: pedido.estoque_ok
data: {"pedido_id":"a1b2c3d4","status":"aguardando pagamento","checkout_url":"https://mock-pagamento.test/checkout/abc"}

```

Tipos já reconhecidos:

- `pedido.estoque_ok`;
- `estoque.indisponivel`;
- `pagamento.aprovado`;
- `pagamento.recusado`;
- `pedido.enviado`.

