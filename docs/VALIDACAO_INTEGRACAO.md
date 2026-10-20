# Validação da integração REST, RabbitMQ e SSE

Data: 20/10/2026.

**Registro da revisão anterior à integração.** Depois desta análise, a versão remota foi incorporada e os contratos de Promoções, dependências e tabela de eventos foram reunidos. Os itens 5 e 8 abaixo descrevem o estado anterior. Para o estado atual, consulte [REQUISITOS_TRABALHO2.md](REQUISITOS_TRABALHO2.md).

**Resultado: há frontend e novos endpoints REST, mas o conjunto ainda não está pronto para demonstrar o fluxo completo do trabalho.** As principais pendências são SSE, contratos entre frontend e Gateway, entrega da URL de checkout e adaptação do consumidor de interesses.

## Versões e alcance

- Base local: `23714f1d9991618d28c8a54e223004f6b047d892`.
- Versão remota analisada: `origin/main`, commit `146999bda419218fb16eca580cb6c5ed4bc2b1c6`.
- As alterações locais do terceiro bloco continuam sem commit e foram preservadas. A versão remota, sozinha, ainda contém o Pagamento e o Promoções anteriores.
- Referência de requisitos: `C:/Users/jjppr/Downloads/Trab2_REST_MOM_SSE_2026_2.pdf`, páginas 1 a 3 e arquitetura da página 4.
- A imagem da conversa foi usada para entender o contrato proposto pelo colega. O PDF prevalece como enunciado; ele não obriga os nomes específicos das rotas HTTP nem o evento adicional `pagamento.criado`.
- A análise remota foi feita em uma cópia dentro de `tmp/validacao-146999b`. Não foi feito pull, merge, commit ou push, nem alteração em código dos serviços na árvore de trabalho.

## O que chegou na branch remota

| Alteração | Local na versão remota |
|---|---|
| Frontend HTML/CSS/JavaScript com catálogo, carrinho, pedidos e formulário de promoções | [frontend](https://github.com/kauangod/ecommerce-web-app/tree/146999bda419218fb16eca580cb6c5ed4bc2b1c6/frontend) |
| REST no Principal, porta 3001: produtos, criação/exclusão de pedidos e cadastro/cancelamento de interesses | [cmd/principal/main.go](https://github.com/kauangod/ecommerce-web-app/blob/146999bda419218fb16eca580cb6c5ed4bc2b1c6/cmd/principal/main.go#L155) |
| Consulta REST ao Estoque e endpoint de estoque na porta 3000 | [consulta no Principal](https://github.com/kauangod/ecommerce-web-app/blob/146999bda419218fb16eca580cb6c5ed4bc2b1c6/cmd/principal/main.go#L243), [servidor no Estoque](https://github.com/kauangod/ecommerce-web-app/blob/146999bda419218fb16eca580cb6c5ed4bc2b1c6/cmd/estoque/main.go#L217) |
| Gravação de estoque em JSON e introdução de um template inicial | [cmd/estoque/estoque.go](https://github.com/kauangod/ecommerce-web-app/blob/146999bda419218fb16eca580cb6c5ed4bc2b1c6/cmd/estoque/estoque.go#L22) |
| Eventos de cadastro e cancelamento com e-mail e uma categoria | [internal/events/dados.go](https://github.com/kauangod/ecommerce-web-app/blob/146999bda419218fb16eca580cb6c5ed4bc2b1c6/internal/events/dados.go#L22), [tipos.go](https://github.com/kauangod/ecommerce-web-app/blob/146999bda419218fb16eca580cb6c5ed4bc2b1c6/internal/events/tipos.go) |
| Janela de cancelamento reduzida de 30 para 10 segundos | [cmd/principal/servico.go](https://github.com/kauangod/ecommerce-web-app/blob/146999bda419218fb16eca580cb6c5ed4bc2b1c6/cmd/principal/servico.go#L13) |

## Pendências para a integração

### 1. SSE e associação do cliente ao pedido: segundo bloco

O Gateway registra apenas rotas REST. Não há endpoint de streaming, gestão de conexões ou associação entre uma conexão do navegador e seus pedidos. O callback de atualização ainda escreve no terminal.

O frontend já usa `EventSource`, mas tenta abrir `/api/eventos`. O teste HTTP do Gateway retornou **404** para `/eventos`.

É necessário implementar o servidor SSE e encaminhar os eventos de estoque, pagamento e entrega ao cliente correto. Também deve ser possível recuperar o estado atual do pedido ao atualizar a página ou reconectar.

### 2. Rotas HTTP e publicação do frontend: primeiro e segundo blocos

| Operação | Frontend atual | Gateway remoto atual |
|---|---|---|
| Produtos | `GET /api/produtos` | `GET /produtos` |
| Listar pedidos | `GET /api/pedidos` | Não implementado; `GET /pedidos` retorna 405 |
| Criar pedido | `POST /api/pedidos` | `POST /pedidos` |
| Cancelar pedido | `DELETE /api/pedidos/{id}` | `DELETE /pedidos/{id}` |
| Cadastrar interesses | `POST /api/interesses-promocao`, corpo com `email` e `categorias[]` | `PUT /interesses/{categoria}`, corpo com `email` |
| Cancelar interesses | `DELETE /api/interesses-promocao`, corpo com `email` e `categorias[]` | `DELETE /interesses/{categoria}?email=...` |
| Notificações | `GET /api/eventos` | Não implementado |

A base `/api` é o padrão de [frontend/js/api.js](https://github.com/kauangod/ecommerce-web-app/blob/146999bda419218fb16eca580cb6c5ed4bc2b1c6/frontend/js/api.js#L1). O HTML não a configura e não há proxy ou servidor de arquivos estáticos configurado no Gateway.

É preciso alinhar essas rotas e definir como servir a loja. Se frontend e Gateway ficarem em origens diferentes, será necessário configurar CORS; o Gateway atual não retorna `Access-Control-Allow-Origin`, e o preflight `OPTIONS /pedidos` retornou 405.

### 3. JSON de produtos e pedidos: primeiro e segundo blocos

- **Produtos:** o Gateway repassa somente `id` e `availability` do Estoque. Faltam nome, preço e categoria do catálogo. O normalizador do frontend não lê `availability`: um produto com estoque zero virou `{nome:"Produto", preco:0, quantidade:null, disponivel:true}`. O Gateway deve combinar catálogo e disponibilidade, e os dois lados precisam usar os mesmos campos.
- **Pedidos:** o Gateway retorna diretamente a struct `Pedido`, com campos JSON `ID`, `Itens`, `ValorTotal`, `Status` e `CriadoEm`. O frontend espera campos em minúsculas. No teste com a resposta real, o pedido virou `{id:"", itens:[], valor_total:0}`. Isso impede a identificação e o acompanhamento do pedido. Definir um DTO de resposta com tags JSON evita expor o modelo interno diretamente.

Evidências: [resposta de produtos](https://github.com/kauangod/ecommerce-web-app/blob/146999bda419218fb16eca580cb6c5ed4bc2b1c6/cmd/principal/main.go#L157), [modelo Pedido](https://github.com/kauangod/ecommerce-web-app/blob/146999bda419218fb16eca580cb6c5ed4bc2b1c6/cmd/principal/pedidos.go#L36), [normalizadores do frontend](https://github.com/kauangod/ecommerce-web-app/blob/146999bda419218fb16eca580cb6c5ed4bc2b1c6/frontend/js/api.js#L64).

### 4. Entrega da URL de checkout: segundo bloco, com ajuste no primeiro

A implementação local de Pagamento publica `pagamento.criado`, com `pedido_id`, `cobranca_id`, `valor_total` e `checkout_url`. Isso acontece depois de `pedido.estoque_ok`; portanto a URL normalmente ainda não existe na resposta inicial de criação do pedido.

O Gateway remoto não faz binding desse evento, não o trata e não armazena a URL. É necessário recebê-lo, verificar sua assinatura e associá-lo ao pedido, disponibilizando a URL ao navegador. O nome desse evento é uma decisão de integração já documentada em [TERCEIRO_BLOCO.md](TERCEIRO_BLOCO.md).

O frontend possui botão que abre uma URL em nova aba. Também lê `checkout_url` de mensagens SSE genéricas. Se o Gateway enviar SSE com `event: pagamento.criado`, será necessário registrar esse listener: atualmente só há listeners nomeados para estoque, aprovação, recusa e envio.

### 5. Interesses em promoções: adaptação necessária no terceiro bloco

| Aspecto | Gateway remoto | Promoções local |
|---|---|---|
| Exchange de interesses | `Promocoes` (`topic`) | Binding em `eCommerce` (`direct`) |
| Cadastro | `interesse.promocao` | `interesse.promocao` |
| Cancelamento | `cancelar.interesse` | `interesse.promocao`, com `acao:"cancelar"` |
| Dados | `{email,categoria}` | `{email,categorias:[],acao}` |

Assim, os eventos publicados pelo novo Gateway não chegam ao binding local de interesses. Mesmo corrigindo apenas a exchange, o corpo atual não passa na validação do repositório local, pois não traz `acao` nem `categorias`.

O ajuste da nossa parte deve receber os dois eventos na exchange acordada e converter cada mensagem para uma operação por categoria. É importante que cadastrar B depois de A preserve A: o contrato local anterior substitui toda a seleção, enquanto o novo endpoint representa uma inscrição por vez.

Fontes: [publicação remota](https://github.com/kauangod/ecommerce-web-app/blob/146999bda419218fb16eca580cb6c5ed4bc2b1c6/cmd/principal/main.go#L118), [bindings locais](../cmd/promocoes/main.go), [validação e persistência locais](../internal/promocoes/interesses.go).

### 6. Tratamento de erros HTTP: segundo bloco

Os handlers novos chamam `FailOnError`, que provoca panic. Foram observados panics para JSON incompleto no cadastro de pedido e para exclusão de pedido inexistente. O Gateway remoto não instala middleware de recuperação de panic.

O ensaio capturou esses panics com um middleware **exclusivo do teste**, permitindo observar os demais casos; o status 500 desse ensaio não é uma garantia de recuperação no servidor original.

Também foi aceito `POST /pedidos` com `{"itens":[]}`: retornou 201 e criou pedido de valor zero. É necessário validar lista não vazia e retornar erros HTTP adequados, sem usar panic para erros de entrada. Convém ainda normalizar o ID para o valor do catálogo: a busca aceita maiúsculas/minúsculas, mas hoje o pedido preserva a grafia recebida e o Estoque usa lookup exato.

### 7. Persistência do Estoque: encaminhar ao Kauan

`saveEstoque` grava `data/estoque.json`, mas `loadEstoque` sempre lê `data/estoque_original.json`. Consequentemente, o serviço reinicia com as quantidades do template e não recupera o estado que salvou.

Essa é uma constatação por leitura do código remoto. Não foi executada uma simulação de reinício desse serviço. O arquivo está nos caminhos protegidos pelo `AGENTS.md`; nenhuma correção foi feita nele.

### 8. Mescla das alterações e teste de eventos

Uma simulação de mescla de três versões, sem escrever nos arquivos de aplicação, identificou conflitos em `.gitignore`, `go.mod`, `go.sum` e `internal/events/tipos.go`.

A integração precisa preservar Fiber e suas dependências remotas, o carregamento de `.env` da parte local e os eventos dos dois lados, sem duplicar `InteressePromocao`. O remoto elevou o requisito do módulo para Go 1.26.0; a documentação de execução deverá acompanhar isso.

Na versão remota, `TestProdutorDoEventoCobreTodosOsTipos` falha: espera 10 tipos, mas a tabela tem 12. Depois de integrar os tipos locais, o teste deve conferir o conjunto completo resultante, incluindo `pagamento.criado` e `cancelar.interesse`.

## Compatibilidade já verificada com o terceiro bloco

- O formato de `pedido.estoque_ok` remoto mantém `pedido_id` e `valor_total`, aceitos pelo Pagamento local.
- Os resultados locais `pagamento.aprovado` e `pagamento.recusado` mantêm os formatos consumidos pelo Principal e pela Entrega existentes.
- As assinaturas continuam cobrindo somente `data`, com assinatura transportada em base64 e verificação com a chave pública do produtor.
- O teste HTTP local cobre criação da cobrança, abertura da página de checkout, aprovação/recusa, Webhook e publicação dos dados esperados pela Entrega. A publicação é substituída por uma função de teste nesse cenário; isso não equivale a executar todos os microsserviços juntos.
- O teste separado com RabbitMQ real confirmou publicação, recebimento e validação de assinatura por uma exchange e uma fila temporárias exclusivas.
- A seleção de destinatários por categoria e o cliente Resend passaram nos testes locais com servidor HTTP simulado. Nenhum e-mail real foi enviado nesta validação.

## Estado dos requisitos do enunciado

| Requisito | Estado observado |
|---|---|
| Frontend em linguagem diferente de Go; catálogo, carrinho, pedidos e formulário de promoções | Interface presente em JavaScript; integração real bloqueada pelos contratos HTTP/JSON |
| REST do Gateway para produtos, pedidos e interesses | Parcial: rotas presentes, com pendências de contrato, listagem e erros |
| Gateway consulta o Estoque por REST | Implementado no remoto; resposta ainda precisa ser combinada com o catálogo |
| Notificações SSE ao cliente correto | Pendente no Gateway |
| Abrir URL de pagamento em nova aba | Código presente no frontend; falta a URL chegar ao pedido no navegador |
| Pagamento cria cobrança via REST no Mock | Implementado localmente e coberto por testes HTTP |
| Mock com página de aprovação/recusa e retorno por Webhook | Implementado localmente e coberto por testes HTTP |
| Pagamento publica aprovação/recusa; Entrega preservada | Contratos compatíveis e testados localmente; falta demonstração completa com Gateway e frontend |
| Promoções registra interesses e envia e-mail externo por categoria | Implementado localmente, mas depende da adaptação ao contrato de interesses do novo Gateway |
| Persistência do Estoque | Gravação presente; leitura no reinício usa o template, pendência descrita acima |
| Eventos RabbitMQ com assinatura assimétrica | Formato compatível; teste real de publicação/assinatura passou |

Esta tabela registra presença de código e limitações; não atesta conformidade integral nem substitui a execução completa para apresentação.

## Testes executados e evidências

| Verificação | Resultado |
|---|---|
| `go test ./...` no código local | Passou |
| `go test ./...` na cópia remota | Falhou somente no teste de contagem de tipos citado acima |
| `TestRabbitMQPublicacaoConfirmadaEAssinada`, com broker real | Passou |
| Rotas extraídas do `main.go` remoto, executadas com `fiber.App.Test` | Confirmaram 404/405 nas rotas ausentes, JSON dos pedidos, pedido vazio aceito e panics |
| Módulo `api.js` real executado em Node com as respostas HTTP observadas | Confirmou perda dos dados do pedido e produto sem estoque marcado como disponível |
| Mescla de três versões em arquivos temporários | Conflitos nos quatro arquivos citados |

Os ensaios HTTP usaram resposta simulada do Estoque e funções de publicação simuladas. Não consumiram filas dos microsserviços nem abriram servidores concorrentes. Não houve validação visual completa no navegador nem teste completo frontend → Gateway → todos os serviços, pois os contratos acima impedem esse fluxo.

As evidências temporárias estão em `tmp/validacao-146999b`: `http_observado.json`, `frontend_observado.json`, `mescla_observada.json` e `auditar_contratos.py`. Para repetir o último ensaio, execute o script Python nessa pasta; ele usa os caminhos locais do Go e do runtime Node desta máquina e remove seu arquivo Go temporário ao terminar.

Como existem essas pendências, o README de conformidade integral solicitado fica para depois da integração e do teste completo. O documento existente [TERCEIRO_BLOCO.md](TERCEIRO_BLOCO.md) continua descrevendo a implementação local; seu contrato de interesses precisará ser atualizado junto com a adaptação do serviço.
