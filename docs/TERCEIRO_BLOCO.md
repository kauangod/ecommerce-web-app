# Terceiro bloco: Pagamento, Mock e Promoções

Esta implementação segue as páginas 2 e 3 de `Trab2_REST_MOM_SSE_2026_2.pdf`. Ela inclui MS Pagamento, Mock externo, MS Promoções e os contratos necessários à integração com o Gateway. O frontend da loja, o Gateway REST/SSE e a consulta REST ao Estoque ficam com os outros blocos.

O código remoto desses blocos foi incorporado até `146999b`. O consumidor de Promoções foi adaptado ao contrato de interesses do Gateway. SSE, repasse do checkout e contratos HTTP do frontend continuam pendentes. Consulte o [mapeamento ponto a ponto do enunciado](REQUISITOS_TRABALHO2.md) para localizar implementações e limitações.

## Organização e responsabilidades

| Local | Responsabilidade |
|---|---|
| `cmd/pagamento` | Inicializar servidor HTTP, consumir estoque e publicar eventos |
| `internal/pagamento` | Regras da cobrança, cliente REST do Mock e Webhook |
| `cmd/mock-pagamento` | Inicializar a aplicação externa, em outro processo |
| `internal/mockpagamento` | Cobranças do provedor, checkout HTML/JavaScript e envio de Webhook |
| `cmd/promocoes` | Gerar ofertas e coordenar consumidores RabbitMQ |
| `cmd/testar-email` | Enviar uma mensagem de teste ao vetor de e-mails do `.env`, sem RabbitMQ |
| `internal/promocoes` | Repositório de interesses, seleção por categoria e cliente Resend |
| `internal/mensageria` | Envelope assinado, validação e publicação com confirmação |
| `internal/httpjson`, `internal/execucao` | Limites HTTP, configuração e encerramento |

Os serviços recebem suas dependências na construção. A interface `Provedor` permite trocar o Mock por outro provedor; `EnviadorEmail` permite testar a seleção de consumidores. As regras de negócio não abrem conexões RabbitMQ. O repositório de interesses só altera a memória depois de gravar o arquivo com sucesso.

## Fluxo de pagamento

1. Estoque publica `pedido.estoque_ok` na exchange `eCommerce`.
2. Pagamento verifica produtor, tipo, routing key e assinatura e solicita uma cobrança ao Mock por REST, informando sua URL de Webhook e um token exclusivo da cobrança.
3. O Mock devolve o identificador e a URL de checkout.
4. Pagamento publica **`pagamento.criado`**, evento adicional com a URL destinada ao Gateway. O log também mostra a URL.
5. Na versão atual, abra a URL do log em uma nova aba e escolha **Pagamento Aprovado** ou **Pagamento Recusado**. A abertura pela loja depende do repasse da URL pelo Gateway, ainda pendente.
6. O servidor do Mock faz POST ao Webhook do MS Pagamento. O navegador não conhece o token desse Webhook.
7. Pagamento publica `pagamento.aprovado` ou `pagamento.recusado`. Os formatos anteriores foram preservados; a Entrega continua consumindo o evento aprovado com `pedido_id` e `valor_total`.

Uma reentrega de estoque não cria uma segunda cobrança para o mesmo pedido. O resultado fica associado a `pedido_id` e `cobranca_id`; o valor aprovado vem do registro da cobrança. Webhooks repetidos não repetem o resultado no mesmo processo. Uma decisão conflitante recebe HTTP 409. Se o broker falhar, o checkout permite reenviar a mesma escolha.

### Contratos REST

Os servidores usam HTTP local para a demonstração. Os clientes têm timeout e não seguem redirecionamentos.

| Processo | Endpoint | Entrada/saída |
|---|---|---|
| Mock | `POST /api/cobrancas` | Recebe criação; devolve cobrança e checkout (201) |
| Mock | `GET /api/cobrancas/{id}` | Dados públicos da cobrança, sem credenciais |
| Mock | `GET /checkout/{id}` | Página de aprovação/recusa |
| Mock | `POST /api/cobrancas/{id}/resultado` | `{"status":"APROVADO"}` ou `{"status":"RECUSADO"}` |
| Pagamento | `POST /webhooks/pagamento` | Recebe resultado e publica evento (200) |

Criação enviada pelo MS Pagamento, com `Authorization: Bearer <MOCK_API_TOKEN>`:

```json
{
  "pedido_id": "pedido-42",
  "valor_total": 199.90,
  "webhook_url": "http://localhost:8082/webhooks/pagamento",
  "webhook_token": "token-exclusivo-da-cobranca"
}
```

O Mock aceita apenas a URL de Webhook configurada no próprio processo. Repetir a criação com os mesmos dados devolve a mesma cobrança; mudar os dados do mesmo pedido resulta em 409.

Webhook enviado pelo Mock, com `Authorization: Bearer <webhook_token>`:

```json
{"cobranca_id":"uuid-da-cobranca","pedido_id":"pedido-42","status":"APROVADO"}
```

## Contratos para quem implementar o Gateway

As duas adições abaixo são decisões de integração, pois o PDF não define o transporte da URL de checkout nem o conteúdo do evento de cancelamento. Os nomes dos eventos de resultado exigidos pelo PDF permanecem iguais.

### Receber a URL de checkout

Adicionar um binding de `fila.principal` para **`pagamento.criado`**, exchange `eCommerce`, produtor `pagamento`. Depois da verificação da assinatura, associar o evento ao pedido correto e encaminhar a URL ao frontend, por exemplo por SSE. Como a cobrança depende da confirmação assíncrona do Estoque, a resposta inicial de criação do pedido pode ainda não ter a URL disponível.

```json
{
  "pedido_id": "pedido-42",
  "cobranca_id": "uuid-da-cobranca",
  "valor_total": 199.90,
  "checkout_url": "http://localhost:8083/checkout/uuid-da-cobranca"
}
```

Este é o conteúdo de `data` de `events.PagamentoCriadoDados`. O Gateway deve verificar a pública de Pagamento, sem chamar o MS Pagamento diretamente. O Gateway incorporado ainda não recebe esse evento: para testar o Mock na versão atual, abra a URL mostrada no log do Pagamento.

### Registrar ou cancelar interesses

Os endpoints públicos existentes no Gateway usam uma categoria por operação. Ambos publicam na exchange **`Promocoes`** (`topic`), com produtor `principal` e o envelope assinado existente:

| Ação | Endpoint do Gateway | Evento |
|---|---|---|
| Cadastrar | `PUT /interesses/{categoria}`, corpo `{"email":"cliente@example.com"}` | `interesse.promocao` |
| Cancelar | `DELETE /interesses/{categoria}?email=cliente%40example.com` | `cancelar.interesse` |

O conteúdo de `data` é `events.InteresseCategoria`:

```json
{"email":"cliente@example.com","categoria":"A"}
```

Promoções vincula `fila.promocoes.interesses` aos dois eventos. Cadastrar A e depois B mantém ambas as categorias; cancelar A preserva B. Uma repetição não duplica a inscrição. O cadastro é persistido em JSON antes de confirmar a mensagem.

Exemplo no Bash, com Gateway e Promoções em execução:

```bash
curl -X PUT http://localhost:3001/interesses/A \
  -H 'Content-Type: application/json' \
  -d '{"email":"cliente@example.com"}'

curl -X DELETE 'http://localhost:3001/interesses/A?email=cliente%40example.com'
```

O contrato anterior continua aceito na exchange `eCommerce`, routing key `interesse.promocao`, para mensagens já enfileiradas e clientes existentes. Nesse formato, `events.InteressePromocaoDados` contém:

```json
{"email":"cliente@example.com","categorias":["A","B"],"acao":"cadastrar"}
```

- No contrato anterior, `cadastrar` substitui a seleção; exige ao menos uma categoria existente no catálogo.
- No contrato anterior, `cancelar` com categorias remove apenas essas categorias.
- No contrato anterior, `cancelar` sem categorias (ou com lista vazia) remove toda a inscrição.
- E-mails são normalizados para minúsculas, categorias para maiúsculas e duplicatas são removidas.

O cancelamento é efetivado quando Promoções consome e persiste o evento. Um envio já em andamento pode terminar. Cadastro/cancelamento repetidos são idempotentes. A fila de interesses é separada da fila de e-mails, então falhas na Resend não bloqueiam a atualização das preferências.

## Promoções e API externa

Promoções mantém a geração e a publicação de ofertas na exchange `Promocoes` (`topic`). C1 e C2 continuam funcionando. A nova fila durável `fila.promocoes.emails` recebe as ofertas e envia uma mensagem individual a cada endereço cadastrado na categoria, sem expor os outros destinatários.

O cliente utiliza [POST /emails da Resend](https://resend.com/docs/api-reference/emails/send-email), com `from`, `to`, `subject` e `text`. O cabeçalho [Idempotency-Key](https://resend.com/docs/dashboard/emails/idempotency-keys) deriva do ID do evento e do destinatário. A Resend mantém essa proteção por 24 horas. Enquanto uma oferta está sendo reprocessada, o serviço também guarda em memória quais destinatários já foram confirmados.

Falhas de rede ou da API devolvem a oferta à fila após uma pausa; os erros ficam no log. Corrija credenciais/remetente e reinicie se a falha for de configuração. As inscrições persistem em JSON local, sem exigir banco de dados.

## Configurar e executar

Execute tudo a partir da raiz do projeto. Use Go 1.26 ou posterior, RabbitMQ e as chaves já geradas. Na primeira instalação, execute `go run ./cmd/gerar-chaves` uma vez; esse comando existente substitui as chaves, portanto não o execute com os serviços em uso.

O arquivo `.env.example` lista as opções. Pagamento, Mock, Promoções e `testar-email` carregam **`.env` automaticamente**, a partir da raiz do projeto. Na primeira instalação, copie o exemplo para `.env` se o arquivo ainda não existir. Variáveis exportadas no terminal têm prioridade sobre o arquivo. O Docker Compose também lê `.env` para configurar o broker.

1. Inicie RabbitMQ: `docker compose up -d`.
2. Em outro terminal: `go run ./cmd/mock-pagamento` (porta 8083).
3. Em outro terminal: `go run ./cmd/pagamento` (Webhook na porta 8082).
4. Inicie Estoque e Entrega pelos comandos existentes.
5. Se quiser manter a demonstração anterior, inicie C1 e C2 antes de Promoções.
6. Configure a Resend no `.env` e inicie Promoções:

```bash
go run ./cmd/promocoes
```

Use remetente autorizado na sua conta e respeite as restrições de destinatários do ambiente de testes da Resend. Não coloque a chave no código nem no Git. Sem chave ou remetente, o processo informa a configuração ausente e encerra.

7. Inicie `go run ./cmd/principal`. Ele mantém o menu de pedidos e expõe o Gateway na porta 3001; o Estoque expõe `/list/storage` na porta 3000. Para testar Pagamento, abra a URL mostrada no log e escolha um resultado. O frontend integrado ao repositório ainda depende dos ajustes listados no mapeamento de requisitos.

| Variável | Padrão | Uso |
|---|---|---|
| `RABBITMQ_URL` | `amqp://ecommerce:ecommerce@localhost:5672/` | Pagamento e Promoções |
| `PAGAMENTO_ADDR` | `127.0.0.1:8082` | Endereço do servidor de Webhook |
| `PAGAMENTO_WEBHOOK_URL` | `http://localhost:8082/webhooks/pagamento` | Deve ser igual em Pagamento e Mock |
| `MOCK_ADDR` | `127.0.0.1:8083` | Endereço do servidor do Mock |
| `MOCK_PUBLIC_URL` | `http://localhost:8083` | Base do cliente REST e das URLs de checkout |
| `MOCK_API_TOKEN` | `desenvolvimento-local` | Deve ser igual em Pagamento e Mock; troque fora da demonstração local |
| `RESEND_API_KEY` | Sem padrão | Obrigatória em Promoções |
| `PROMOCOES_EMAIL_FROM` | Sem padrão | Obrigatória em Promoções |
| `PROMOCOES_EMAILS_TESTE` | Lista vazia | Vetor JSON de strings usado somente pelo comando de teste |
| `PROMOCOES_INTERESSES_PATH` | `data/interesses.local.json` | Estado local ignorado pelo Git |
| `PROMOCOES_INTERVALO` | Aleatório, 20 a 30 segundos | Duração fixa opcional, por exemplo `25s` |

### Arquivo de configuração e teste de e-mail

A chave fica em `RESEND_API_KEY`, no arquivo `.env` da raiz. Esse arquivo é ignorado pelo Git. O modelo versionado `.env.example` contém apenas exemplos, sem a chave real.

```dotenv
RESEND_API_KEY=sua-chave-da-resend
PROMOCOES_EMAIL_FROM="E-commerce <onboarding@resend.dev>"
PROMOCOES_EMAILS_TESTE='["primeiro@example.com","segundo@example.com"]'
```

`PROMOCOES_EMAILS_TESTE` é um vetor de strings que o Go converte em `[]string`. Para adicionar destinatários, inclua novos endereços entre aspas dentro dos colchetes. O comando abaixo lê o `.env`, chama a API e envia uma mensagem individual para cada endereço, uma única vez por execução:

```bash
go run ./cmd/testar-email
```

Esse teste não depende de RabbitMQ e não cria inscrições permanentes. As promoções normais continuam usando as categorias e os interesses recebidos por evento. Os testes automatizados usam um servidor HTTP simulado e não enviam mensagens reais.

Com o remetente `onboarding@resend.dev`, a Resend permite testar envios para o endereço da própria conta. Para enviar a outros destinatários, configure um [domínio verificado e um remetente desse domínio](https://resend.com/docs/knowledge-base/403-error-resend-dev-domain) em `PROMOCOES_EMAIL_FROM`. O comando apresenta a explicação devolvida pela API quando um envio é recusado.

## Testes e limites

```powershell
go test ./...
go vet ./...
```

Os testes HTTP usam servidores locais reais (`httptest`) e substituem apenas a publicação RabbitMQ por um gravador. Cobrem aprovação/recusa, contratos da Entrega, repetição, falha de publicação, proteção do Webhook, persistência/cancelamento e requisições Resend sem enviar e-mails reais.

O teste opcional de transporte exige um broker real. Ele cria somente exchange e fila temporárias:

```powershell
$env:RABBITMQ_TEST_URL = 'amqp://ecommerce:ecommerce@localhost:5672/'
go test ./internal/mensageria -run TestRabbitMQ -v
go test ./cmd/principal -run TestGatewayRabbitMQPromocoes -v
```

O segundo teste usa as funções de cadastro/cancelamento do Gateway, sua publicação assinada, uma exchange `topic` temporária e o adaptador de Promoções. Verifica cadastro cumulativo, repetição, cancelamento por categoria e persistência, sem chamar Resend nem consumir filas da aplicação.

Cobranças e resultados de Pagamento e Mock ficam **em memória**, assim como os pedidos do Principal. Se reiniciar esses processos durante uma compra, reinicie os dois e faça um novo pedido; recuperação de cobranças entre reinícios exigiria persistência adicional. As inscrições de Promoções persistem. A publicação local de Pagamento e Promoções usa confirmação do broker, mas ainda admite reentrega: os consumidores devem tratar eventos repetidos pelo identificador do pedido/cobrança. A integração completa da loja ainda depende de SSE, repasse da URL do checkout e alinhamento HTTP entre frontend e Gateway.
