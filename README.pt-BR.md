# Aluguel de Carros com Blockchain

Uma pequena blockchain escrita do zero em Go, na qual o depósito do locatário e os itens
empenhados são tokens mantidos em custódia (escrow) por um "contrato inteligente" em Go. O design
está em [Design.pt-BR.md](Design.pt-BR.md) (ou [Design.md](Design.md), em inglês).

```text
chain/                 módulo Go "carrental"
  cmd/node/            o nó da blockchain (API HTTP na porta :8080)
  cmd/seed/            registra os dados de demonstração
  cmd/tx/              assina uma transação a partir do shell, para as demos com curl
  pkg/contract/        o contrato: tokens, papéis, regras de locação e liquidação (Dispatch)
  pkg/chain/           transações assinadas, blocos encadeados por hash, repetição a partir do disco
  pkg/api/             servidor e cliente HTTP
  pkg/crypto/          chaves Ed25519, hashes SHA-256
  keys/                chaves de desenvolvimento conhecidas (veja abaixo)
web/index.html         o app web: Register, Fleet, Rent, Chain Explorer
meta/                  documentos off-chain; só o SHA-256 deles fica on-chain
docker-compose.yml
```

## Como executar

Com Docker:

```bash
docker compose up --build
# chain http://localhost:8080 · web http://localhost:3000 · meta http://localhost:3001
docker compose down        # a cadeia vive em memória, então isso a reinicia do zero
```

Sem Docker (Go 1.23+), a partir de `chain/`:

```bash
go run ./cmd/node                        # terminal 1; adicione --datadir data para manter os blocos em disco
go run ./cmd/seed                        # terminal 2
npx http-server ../web  -p 3000 --cors -c-1
npx http-server ../meta -p 3001 --cors -c-1
```

Depois abra http://localhost:3000. O app web carrega o `tweetnacl` a partir do jsDelivr, então o
navegador precisa de acesso à internet.

## Contas

`chain/keys/` guarda três chaves de desenvolvimento: **admin** (o validador, com os papéis ADMIN,
REGISTRAR e INSPECTOR), **alice** (proprietária do carro) e **bob** (locatário). Cada arquivo é uma
seed Ed25519 em hexadecimal igual a `SHA-256("car-rental dev account: <nome>")`. O app web deriva
as mesmas chaves para o seu seletor de contas. `validator.key` e `admin.key` são a mesma chave.
Como as contas padrão do Hardhat, essas chaves são públicas, então nunca as use para proteger algo
de valor.

## Demo a partir do shell (Design.md, seção 9)

A partir de `chain/`, com o nó em execução e com os dados carregados:

```bash
NODE=http://127.0.0.1:8080
BOB=$(go run ./cmd/tx --key keys/bob.key --address)
HASH=$(sha256sum ../meta/checkout.json | cut -d' ' -f1)
sign() { go run ./cmd/tx --key "keys/$1.key" --method "$2" --args "$3"; }

# 9.3 antes
curl $NODE/assets/1; curl $NODE/balance/0/$BOB

# 9.2 inicia uma locação: bob bloqueia 500 tokens de depósito e o relógio (item 2)
curl $NODE/tx -d "$(sign bob StartRental '{"CarID":1,"DaysPlanned":3,"Items":[2],"Deposit":500,"CheckoutHash":"'$HASH'"}')"

# 9.3 depois
curl $NODE/assets/1; curl $NODE/balance/0/$BOB; curl $NODE/balance/0/escrow; curl $NODE/rentals/1

# 9.4 inválido: entra no bloco com erro, estado inalterado
curl $NODE/tx -d "$(sign bob StartRental '{"CarID":1,"DaysPlanned":2,"Items":[],"Deposit":100}')"   # carro indisponível
curl $NODE/tx -d "$(sign bob StartRental '{"CarID":3,"DaysPlanned":1,"Items":[],"Deposit":50}')"    # garantia insuficiente

# 9.5a assinatura inválida: HTTP 400, nunca entra no bloco
curl -i $NODE/tx -d '{"from":"'$BOB'","method":"RegisterCar","args":{},"nonce":4,"signature":"0000garbage"}'

# 9.5b assinatura válida, papel ausente: entra no bloco com erro
curl $NODE/tx -d "$(sign bob RegisterCar '{"Owner":"'$BOB'","DailyRate":100,"MinCollateral":1000}')"
curl $NODE/tx -d "$(sign bob SettleRental '{"RentalID":1,"DamageCharge":0}')"

# liquidar como o vistoriador (inspector)
curl $NODE/tx -d "$(sign admin SettleRental '{"RentalID":1,"DamageCharge":150}')"
```

O app web consulta o nó periodicamente, então as transações enviadas pelo shell aparecem no Chain
Explorer em poucos segundos. Para ver o selo vermelho "document changed" na aba Fleet, edite um
arquivo em `meta/` e clique em **Re-check documents**.

## API

| Endpoint | Retorna |
| --- | --- |
| `POST /tx` | `{txHash, block, receipt}`; 400 para transação malformada ou assinatura inválida, 409 para transação repetida |
| `GET /assets`, `GET /assets/{id}` | carros e itens de garantia, com o `holder` (detentor) atual |
| `GET /balance/{token}/{addr}` | um número; o token 0 é o token de depósito, `addr` pode ser `escrow` |
| `GET /rentals`, `GET /rentals/{id}` | registros de locação |
| `GET /roles/{addr}` | ex.: `["ADMIN","INSPECTOR","REGISTRAR"]` |
| `GET /blocks?count=N` | blocos mais recentes primeiro, com transações e recibos decodificados |

Métodos do contrato: `MintDeposit`, `RegisterCar`, `RegisterCollateral`, `Transfer`, `StartRental`,
`SettleRental`. Uma transação assina `JSON.stringify({from, method, args, nonce})`, com hash
SHA-512, usando Ed25519.

## Testes

```bash
cd chain && go test ./...
```

Os testes cobrem a tabela do exemplo resolvido da seção 5, devoluções em atraso, todos os caminhos
de rejeição (verificando que o hash do estado permanece inalterado), o roteiro da seção 9 via HTTP,
assinatura no estilo do navegador, e reinício a partir do disco, incluindo logs de blocos
adulterados.

## Onde o código se afasta do Design.md

- **Horário do bloco, não `time.Now()`.** O `Dispatch` recebe o timestamp do bloco, para que
  reexecutar a cadeia mais tarde calcule as mesmas datas de vencimento e as mesmas multas por
  atraso.
- **Verificações que faltavam no esboço.** O `StartRental` verifica se o locatário realmente possui
  o depósito e rejeita um item empenhado duas vezes na mesma chamada, o que contaria seu valor em
  dobro. A aritmética é verificada contra overflow, e argumentos malformados são rejeitados em vez
  de lidos silenciosamente como zero.
- **Método `Transfer`.** Ele aplica a regra de que um carro alugado não pode ser vendido, que o
  esboço da seção 5 listava mas para a qual não havia método.
- **Transações rejeitadas não mudam nada.** Cada transação roda sobre uma cópia do estado, que só é
  mantida se o contrato a aceitar.
- **Dados de seed.** O script de seed também registra o carro 3 (50/dia, garantia mínima 1200),
  usado na seção 9.4. O `MintDeposit` emite um evento `DepositMinted`, e o `RentalSettled` reporta
  qualquer cobrança `unpaid` (não paga) restante depois que todos os itens foram confiscados.
- **Sem `pkg/state`.** O estado e o ledger vivem em `pkg/contract`, já que a seção 3 diz que o
  pacote dono do ledger também é dono das regras.
- **Proteção e verificação contra repetição.** O nó recusa uma transação cujo hash já esteja na
  cadeia. Cada bloco registra o hash do estado e o hash dos recibos, e `--datadir` reexecuta e
  verifica cada bloco ao iniciar.
