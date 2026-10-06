# Guia de Conversão: Blockchain Java → Go

Este guia descreve como portar o projeto Java (`Block`, `Blockchain`, `Utils`, `Main`) para Go, mapeando cada classe para o equivalente idiomático na nova linguagem.

## 1. Estrutura de pastas sugerida

```
blockchain-go/
├── go.mod
├── main.go
└── blockchain/
    ├── block.go
    ├── blockchain.go
    └── utils.go
```

Rode `go mod init blockchain-go` na raiz para criar o `go.mod`.

## 2. Mapeamento geral de conceitos

| Java                               | Go                                                              |
|-------------------------------------|------------------------------------------------------------------|
| `class Block` com getters           | `struct Block` com campos exportados (ou métodos getters simples) |
| `class Blockchain` com `List<Block>` | `struct Blockchain` com `[]*Block`                               |
| `class Utils` (métodos estáticos)   | Funções de pacote em `utils.go` (sem necessidade de classe)       |
| `MessageDigest` (SHA-256)            | Pacote `crypto/sha256` da stdlib                                  |
| `System.currentTimeMillis()`        | `time.Now().UnixMilli()`                                         |
| `StringBuilder`                     | `strings.Builder` ou `fmt.Sprintf`                                |
| Exceptions (`NoSuchAlgorithmException`) | Go usa `error` como retorno — SHA-256 nunca falha, então pode ser ignorado |
| `toString()`                        | Implementar `String() string` (interface `fmt.Stringer`)         |

## 3. `block.go`

```go
package blockchain

import (
    "crypto/sha256"
    "encoding/hex"
    "fmt"
    "strconv"
    "time"
)

type Block struct {
    Index        int
    Timestamp    int64
    Hash         string
    PreviousHash string
    Data         string
    Nonce        int
}

// NewBlock cria um bloco e já calcula seu hash inicial.
func NewBlock(index int, timestamp int64, previousHash string, data string) *Block {
    b := &Block{
        Index:        index,
        Timestamp:    timestamp,
        PreviousHash: previousHash,
        Data:         data,
        Nonce:        0,
    }
    b.Hash = CalculateHash(b)
    return b
}

func (b *Block) str() string {
    return strconv.Itoa(b.Index) + strconv.FormatInt(b.Timestamp, 10) +
        b.PreviousHash + b.Data + strconv.Itoa(b.Nonce)
}

func (b *Block) String() string {
    return fmt.Sprintf("Block #%d [previousHash : %s, timestamp : %s, data : %s, hash : %s]",
        b.Index, b.PreviousHash, time.UnixMilli(b.Timestamp), b.Data, b.Hash)
}

// CalculateHash equivale ao Block.calculateHash(Block) estático em Java.
func CalculateHash(b *Block) string {
    if b == nil {
        return ""
    }
    sum := sha256.Sum256([]byte(b.str()))
    return hex.EncodeToString(sum[:])
}

// ProofOfWork equivale ao método proofOfWork(int) em Java.
func (b *Block) ProofOfWork(difficulty int) {
    b.Nonce = 0
    target := Zeros(difficulty)
    for b.Hash[:difficulty] != target {
        b.Nonce++
        b.Hash = CalculateHash(b)
    }
}
```

**Observações:**
- `PreviousHash == ""` substitui `previousHash == null` do Java (Go strings não têm `nil`). Ajuste as validações em `blockchain.go` de acordo.
- `hex.EncodeToString` já produz hex com zero-padding, equivalente ao loop manual de `Integer.toHexString` em Java.

## 4. `utils.go`

```go
package blockchain

import "strings"

// Zeros equivale a Utils.zeros(int) em Java.
func Zeros(length int) string {
    return strings.Repeat("0", length)
}
```

## 5. `blockchain.go`

```go
package blockchain

import (
    "strings"
    "time"
)

type Blockchain struct {
    Difficulty int
    Blocks     []*Block
}

// NewBlockchain equivale ao construtor Blockchain(int) em Java.
func NewBlockchain(difficulty int) *Blockchain {
    genesis := NewBlock(0, time.Now().UnixMilli(), "", "Block genesis")
    genesis.ProofOfWork(difficulty)

    return &Blockchain{
        Difficulty: difficulty,
        Blocks:     []*Block{genesis},
    }
}

func (bc *Blockchain) LatestBlock() *Block {
    return bc.Blocks[len(bc.Blocks)-1]
}

// NewBlockData equivale a Blockchain.newBlock(String) em Java.
func (bc *Blockchain) NewBlockData(data string) *Block {
    latest := bc.LatestBlock()
    return NewBlock(latest.Index+1, time.Now().UnixMilli(), latest.Hash, data)
}

func (bc *Blockchain) AddBlock(b *Block) {
    if b == nil {
        return
    }
    b.ProofOfWork(bc.Difficulty)
    bc.Blocks = append(bc.Blocks, b)
}

func (bc *Blockchain) isFirstBlockValid() bool {
    first := bc.Blocks[0]

    if first.Index != 0 {
        return false
    }
    if first.PreviousHash != "" {
        return false
    }
    if first.Hash == "" || CalculateHash(first) != first.Hash {
        return false
    }
    return true
}

func isValidNewBlock(newBlock, previousBlock *Block) bool {
    if newBlock == nil || previousBlock == nil {
        return false
    }
    if previousBlock.Index+1 != newBlock.Index {
        return false
    }
    if newBlock.PreviousHash == "" || newBlock.PreviousHash != previousBlock.Hash {
        return false
    }
    if newBlock.Hash == "" || CalculateHash(newBlock) != newBlock.Hash {
        return false
    }
    return true
}

func (bc *Blockchain) IsBlockChainValid() bool {
    if !bc.isFirstBlockValid() {
        return false
    }
    for i := 1; i < len(bc.Blocks); i++ {
        if !isValidNewBlock(bc.Blocks[i], bc.Blocks[i-1]) {
            return false
        }
    }
    return true
}

func (bc *Blockchain) String() string {
    var sb strings.Builder
    for _, b := range bc.Blocks {
        sb.WriteString(b.String())
        sb.WriteString("\n")
    }
    return sb.String()
}
```

## 6. `main.go`

```go
package main

import (
    "fmt"

    "blockchain-go/blockchain"
)

func main() {
    bc := blockchain.NewBlockchain(4)
    bc.AddBlock(bc.NewBlockData("Tout sur le Bitcoin"))
    bc.AddBlock(bc.NewBlockData("Sylvain Saurel"))
    bc.AddBlock(bc.NewBlockData("https://www.toutsurlebitcoin.fr"))
    bc.AddBlock(bc.NewBlockData("https://www.uea.edu.br"))

    fmt.Println(bc)

    fmt.Println("Blockchain é válido?")
    if !bc.IsBlockChainValid() {
        fmt.Println("Não é válido!!!")
    } else {
        fmt.Println("Sim é válido!!!")
    }
}
```

## 7. Diferenças importantes a lembrar

1. **`null` → zero values**: `previousHash == null` vira `previousHash == ""`. Cuidado para não confundir hash vazio com hash real.
2. **Sem exceptions checadas**: `NoSuchAlgorithmException` não existe em Go; `sha256` da stdlib sempre funciona, então `CalculateHash` não precisa retornar `error`.
3. **Ponteiros vs valores**: use `*Block` (ponteiro) para permitir mutação do `Hash`/`Nonce` dentro de `ProofOfWork` e para evitar cópias custosas.
4. **Concorrência**: Go facilita usar goroutines para mineração paralela, caso deseje evoluir o projeto (fora do escopo direto desta conversão).
5. **Testes**: migre a lógica de `Main` para testes com `go test`, criando `blockchain_test.go` com casos como "bloco inválido corrompe a chain" (o trecho comentado em `Main.java`).
6. **Formatação de tempo**: `time.UnixMilli(ts)` substitui `new Date(timestamp)`; ajuste o layout de impressão com `.Format(...)` se quiser um formato específico.

## 8. Passos sugeridos de migração

1. Criar o módulo Go (`go mod init`) e a estrutura de pastas.
2. Portar `Utils.java` → `utils.go` (mais simples, sem dependências).
3. Portar `Block.java` → `block.go`, validando o cálculo de hash com um teste unitário comparando saída com a versão Java (mesma entrada deve gerar mesmo SHA-256).
4. Portar `Blockchain.java` → `blockchain.go`.
5. Portar `Main.java` → `main.go`.
6. Rodar `go build ./...` e `go vet ./...` para validar.
7. (Opcional) Adicionar testes em `blockchain_test.go` cobrindo `IsBlockChainValid`, incluindo o cenário de bloco inválido comentado no `Main.java` original.
