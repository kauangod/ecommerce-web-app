

## Modo de demonstração

Abra `http://localhost:<porta>/?demo=1` para demonstrar a interface sem o
backend. Nesse modo, catálogo, pedidos e interesses em promoções usam apenas o
armazenamento local do navegador. Sem `?demo=1`, a interface usa os endpoints
REST e SSE reais descritos abaixo.

## Contrato esperado do API Gateway

Todos os erros REST devem usar um status HTTP adequado e, preferencialmente,
um JSON como `{ "erro": "mensagem para o usuário" }`.

### Catálogo

`GET /produtos`

Resposta: um array com todos os produtos do catálogo e a quantidade do MS
Estoque:

```json
{
  "id": "P01",
  "nome": "Fone de ouvido Bluetooth",
  "categoria": "A",
  "preco": 199.90,
  "availability": 8
}
```

### Pedidos

`POST /pedidos`

```json
{
  "email": "cliente@exemplo.com",
  "itens": [
    { "produto_id": "P01", "quantidade": 2 }
  ]
}
```

Resposta `201 Created`: o pedido abaixo. `CheckoutURL` é opcional na criação;
se a cobrança só existir depois de `pedido.estoque_ok`, a URL deve chegar em uma
notificação SSE posterior.

`GET /pedidos?email=cliente%40exemplo.com` retorna os pedidos do cliente. Sem
o parâmetro, mantém a listagem completa para compatibilidade e demonstração.

`DELETE /pedidos/{id}` cancela o pedido e responde `202 Accepted`.

```json
{
  "ID": "a1b2c3d4",
  "Email": "cliente@exemplo.com",
  "Itens": [
    {
      "ProdutoID": "P01",
      "Nome": "Fone de ouvido Bluetooth",
      "Quantidade": 2,
      "PrecoUnitario": 199.90
    }
  ],
  "ValorTotal": 399.80,
  "Status": "aguardando confirmação",
  "Motivo": "",
  "Detalhe": "",
  "CobrancaID": "abc",
  "CheckoutURL": "http://localhost:8083/checkout/abc",
  "CriadoEm": "2026-10-19T20:30:00Z"
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

`PUT /interesses/{categoria}` recebe uma categoria por chamada:

```json
{
  "email": "cliente@exemplo.com"
}
```

`DELETE /interesses/{categoria}?email=cliente%40exemplo.com` cancela sem body.
Para várias categorias, o frontend faz uma chamada separada para cada uma.

### Atualizações SSE

`GET /eventos?email=cliente%40exemplo.com` responde com
`Content-Type: text/event-stream`, mantém a
conexão aberta e envia uma linha `id:` para permitir reconexão com
`Last-Event-ID`. O Gateway envia apenas eventos dos pedidos associados ao
e-mail informado. O frontend aceita eventos nomeados ou mensagens genéricas.

Exemplo nomeado:

```text
id: 40c665bb
event: pedido.estoque_ok
data: {"pedido_id":"a1b2c3d4","status":"aguardando pagamento","checkout_url":"https://mock-pagamento.test/checkout/abc"}

```

Tipos já reconhecidos:

- `pedido.estoque_ok`;
- `estoque.indisponivel`;
- `pagamento.criado`;
- `pagamento.aprovado`;
- `pagamento.recusado`;
- `pedido.enviado`.

