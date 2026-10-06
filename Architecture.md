# Blockchain Study Project: Porting a Java Blockchain to Go

A study project to learn how a blockchain works. It is built in three phases:

1. **Port.** Recode the small Java blockchain in [blockchain-example/](blockchain-example/) in Go,
   as a line-by-line translation (blocks, hash linking, proof of work, validation).
2. **Domain.** Put a small car-rental token ledger on top of that chain: deposit tokens, cars,
   rentals and three roles (administrator, owner, renter).
3. **UI.** A web interface in Brazilian Portuguese where the user can **act as** the administrator,
   an owner or a renter, plus a **tab to inspect the blockchain** itself.

The earlier car-rental prototype (HTTP node, Ed25519 transactions, escrow contract) is no longer in
the working tree. It is still in git history (commit `da716cb`). Phases 2 and 3 reuse its idea in a
much smaller form, and it can be a reference for the follow-ups in section 10.

## Quick start

The system is implemented in [car-rent-blockchain/](car-rent-blockchain/) (Go 1.23+, standard
library only):

```bash
cd car-rent-blockchain
go test ./...                       # blockchain, rental rules and HTTP API tests
go run ./cmd/console                # phase 1: the Java demo, ported
go run ./cmd/server --debug         # phases 2 and 3: http://localhost:8080 (pt-BR UI)
```

Server flags: `--addr` (default `:8080`), `--difficulty` (default `3`) and `--debug`, which enables
the block tamper demo in the explorer tab. The chain lives in memory, so restarting the server
starts a new chain.

## 1. Goals

1. Understand the core blockchain mechanics: blocks, hash linking, proof of work, chain validation.
2. Produce a Go port that behaves the same as the Java original, including identical SHA-256 hashes
   for identical block input.
3. Cover the port with `go test`, including the "corrupted chain" scenario that is commented out in
   `Main.java`.
4. Keep it dependency-free (Go standard library only).
5. Build a web UI where one person can switch between the **Administrador**, **Proprietário** and
   **Locatário** roles and see what each role is allowed to do.
6. Make the chain visible: a UI tab that lets the user inspect every block, check that the chain is
   valid and see what happens when a block is tampered with.
7. Show everything the user sees in Brazilian Portuguese (pt-BR), see below.

### Language policy

- **The UI is in Brazilian Portuguese (pt-BR).** That means the whole web app (section 8): page
  title, role names, tab names, buttons, form labels, messages, error messages and the date format
  (`dd/MM/yyyy HH:mm:ss`). The `<html>` tag carries `lang="pt-BR"`. The console demo from phase 1
  is pt-BR as well (labels such as `Bloco`, `hashAnterior`, `dados`, the genesis text
  `Bloco gênesis`, and the messages `Sim, é válida!` / `Não é válida!`), which matches the Java
  original whose messages were already in Portuguese.
- **Everything else is in English:** this README, Go identifiers, code comments and test names.
- Keep user-facing strings out of the logic where practical, so a later translation or an HTTP/web
  front end can reuse them. In the web app every string lives in one `T` object in `index.html`.
  Tests must not depend on the Portuguese wording.

Non-goals: multiple nodes or consensus between nodes, real wallets and signatures, real money.
Phases 1 to 3 run a single node in one process. Signatures and peers are listed as follow-ups in
section 10.

## 2. What the Java example does

| File | Role |
| --- | --- |
| [Block.java](blockchain-example/src/Block.java) | A block: `index`, `timestamp`, `hash`, `previousHash`, `data`, `nonce`. Computes its own SHA-256 and mines itself (`proofOfWork`). |
| [Blockchain.java](blockchain-example/src/Blockchain.java) | A list of blocks plus a mining `difficulty`. Creates the genesis block, builds and adds blocks, validates the whole chain. |
| [Utils.java](blockchain-example/src/Utils.java) | `zeros(n)`: a string of `n` zeros, used as the proof-of-work target. |
| [Main.java](blockchain-example/src/Main.java) | Builds a chain with difficulty 4, adds four blocks, prints it and checks validity. |

How it fits together:

- **Hash input.** `Block.str()` returns `index + timestamp + previousHash + data + nonce`, and
  `calculateHash` returns the hex SHA-256 of that string.
- **Mining.** `proofOfWork(d)` resets `nonce` to 0 and increments it, rehashing each time, until the
  hash starts with `d` zeros.
- **Adding.** `newBlock(data)` links a new block to the latest one (`previousHash = latest.hash`)
  without mining it. `addBlock(b)` mines it, then appends it. It does **not** validate the block.
- **Validation.** `isBlockChainValid()` checks the genesis block (index 0, no previous hash, stored
  hash equals recomputed hash), then for each later block checks that the index is the previous
  index plus one, `previousHash` equals the previous block's hash, and the stored hash equals the
  recomputed hash.

## 3. Java to Go mapping

| Java | Go |
| --- | --- |
| `class Block` with private fields and getters | `struct Block` with exported fields |
| `class Blockchain` with `List<Block>` | `struct Blockchain` with `[]*Block` |
| `class Utils` with static methods | plain package function `Zeros` in `utils.go` |
| `static Block.calculateHash(Block)` | package function `CalculateHash(*Block)` |
| `MessageDigest` (SHA-256) + manual hex loop | `crypto/sha256` + `encoding/hex` |
| `NoSuchAlgorithmException` handling | not needed: `sha256.Sum256` cannot fail |
| `null` previous hash | empty string `""` |
| `System.currentTimeMillis()` | `time.Now().UnixMilli()` |
| `new Date(timestamp)` | `time.UnixMilli(timestamp)` |
| `StringBuilder` | `strings.Builder` / `fmt.Sprintf` |
| `toString()` | `String() string` (the `fmt.Stringer` interface) |
| `Blockchain.newBlock(String)` | `(*Blockchain).NewBlock(string)` |
| `isBlockChainValid()` | `(*Blockchain).IsValid()` |

Naming note: Go has no overloading, so the package function `NewBlock` (constructor) and the method
`(*Blockchain).NewBlock` (build next block) coexist because one is a function and the other a method.

## 4. Layout

```text
car-rent-blockchain/
├── go.mod
├── blockchain/                # phase 1: the port of the Java example (the core)
│   ├── utils.go               #   Utils.java
│   ├── block.go               #   Block.java
│   ├── blockchain.go          #   Blockchain.java
│   └── blockchain_test.go
├── cmd/
│   ├── console/main.go        # phase 1: Main.java
│   └── server/main.go         # phase 3: starts the HTTP server and the web UI
├── rental/                    # phase 2: transactions, roles, rules, state
│   ├── tx.go                  #   transaction envelope and strict JSON decoding
│   ├── state.go               #   accounts, balances, cars, rentals
│   ├── errors.go              #   rule violations, with stable English codes
│   ├── rules.go               #   the five methods and Apply
│   ├── messages_ptbr.go       #   every pt-BR sentence of this package
│   ├── validate.go            #   Replay and ValidateChain
│   └── rental_test.go
├── api/                       # phase 3: JSON handlers
│   ├── server.go
│   └── server_test.go
└── web/                       # phase 3: the pt-BR UI
    ├── index.html             #   single page, vanilla JS, no build step
    └── web.go                 #   //go:embed index.html
```

`web/web.go` exists because `go:embed` cannot reach files outside its own package directory.

The Go module is `car-rent-blockchain`. Everything targets Go 1.23+ and uses only the standard library. The `blockchain` package stays free of any
knowledge about rentals, JSON or HTTP, so the port remains a faithful copy of the Java code.

## 5. Porting pitfalls (read before translating)

1. **`index + timestamp` is arithmetic in Java, not concatenation.** `index + timestamp +
   previousHash + ...` is evaluated left to right, so the `int` and `long` are added as numbers
   (`0 + 1700000000000` becomes `1700000000000`) before the first `String` is involved. Naively
   writing `strconv.Itoa(index) + strconv.FormatInt(timestamp, 10)` in Go gives `01700000000000`
   and a different hash. The port below adds them first to stay compatible.
2. **A `null` previous hash is printed as `"null"`.** Java string concatenation turns `null` into
   the text `null`, so the genesis block hashes `...null...`. In Go the genesis `PreviousHash` is
   `""`, so `hashInput` substitutes `"null"` when it is empty.
3. **Both quirks are weaknesses, not features.** Without a separator, different field values can
   produce the same hash input (for example index 1 + timestamp 20 and index 12 + timestamp 9 both
   give `21`). They are kept on purpose so the Go and Java hashes match; the hardening step in
   section 9 removes them once parity has been checked.
4. **Do not slice the hash for the difficulty check.** Java's
   `getHash().substring(0, difficulty)` would panic in Go (`b.Hash[:difficulty]`) for a difficulty
   above 64. `strings.HasPrefix` is safe and clearer.
5. **Character encoding.** Java's `getBytes()` uses the platform default charset, while Go strings
   are UTF-8. Test vectors should be plain ASCII. The Java sources are saved as ISO-8859-1, which is
   why the accented text in `Blockchain.java` and `Main.java` shows up garbled; the Go files are
   UTF-8, so the pt-BR text (`gênesis`, `válido`, `Não`) is written with its proper accents. Make sure
   the editor saves the files as UTF-8; Go source requires it.
6. **Pointers.** Use `*Block` so `ProofOfWork` can update `Hash` and `Nonce` in place and so the
   chain does not copy blocks.
7. **Mining time.** Difficulty 4 needs about 65,000 hashes per block on average (16^4), which is
   still instant. Each extra level multiplies the expected work by 16, so keep tests at difficulty
   2 or 3, and let the web server default to 3 so every transaction confirms quickly.
8. **`ProofOfWork` trusts the current `Hash`.** Like the Java original, it only recomputes the hash
   after the first failed check, so calling it on a block whose data was edited, without refreshing
   `Hash` first, can return immediately with a stale hash. New blocks are fine (the constructor
   sets `Hash`). Code that re-mines an edited block must set `Nonce = 0` and
   `Hash = CalculateHash(b)` first; the rental tests do.
9. **`IsValid` does not check the proof of work.** The Java validator only checks index, links and
   that the stored hash equals the recomputed one. A last block whose hash lacks the leading zeros
   still passes. `rental.ValidateChain` adds that check.

## 6. The Go code

### `blockchain/utils.go`

```go
package blockchain

import "strings"

// Zeros mirrors Utils.zeros(int): a string of `length` zeros.
func Zeros(length int) string {
	return strings.Repeat("0", length)
}
```

### `blockchain/block.go`

```go
package blockchain

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Block mirrors Block.java. An empty PreviousHash plays the role of Java's null.
type Block struct {
	Index        int
	Timestamp    int64 // Unix milliseconds
	Hash         string
	PreviousHash string
	Data         string
	Nonce        int
}

// NewBlock mirrors the Java constructor: nonce 0 and an initial (unmined) hash.
func NewBlock(index int, timestamp int64, previousHash, data string) *Block {
	b := &Block{
		Index:        index,
		Timestamp:    timestamp,
		PreviousHash: previousHash,
		Data:         data,
	}
	b.Hash = CalculateHash(b)
	return b
}

// hashInput mirrors Block.str(). Java evaluates `index + timestamp + previousHash + ...`
// left to right, so index and timestamp are ADDED as numbers before the first string
// joins in, and a null previousHash is printed as "null". Both quirks are kept so the
// Go port produces the same SHA-256 as the Java original.
func (b *Block) hashInput() string {
	prev := b.PreviousHash
	if prev == "" {
		prev = "null"
	}
	return strconv.FormatInt(int64(b.Index)+b.Timestamp, 10) + prev + b.Data + strconv.Itoa(b.Nonce)
}

// CalculateHash mirrors the static Block.calculateHash(Block): hex SHA-256 of hashInput.
func CalculateHash(b *Block) string {
	if b == nil {
		return ""
	}
	sum := sha256.Sum256([]byte(b.hashInput()))
	return hex.EncodeToString(sum[:])
}

// ProofOfWork mirrors proofOfWork(int): bump Nonce until Hash starts with `difficulty` zeros.
func (b *Block) ProofOfWork(difficulty int) {
	b.Nonce = 0
	target := Zeros(difficulty)
	for !strings.HasPrefix(b.Hash, target) {
		b.Nonce++
		b.Hash = CalculateHash(b)
	}
}

// String mirrors toString(). The text is user-facing, so it is in pt-BR.
func (b *Block) String() string {
	return fmt.Sprintf("Bloco #%d [hashAnterior : %s, dataHora : %s, dados : %s, hash : %s]",
		b.Index, b.PreviousHash, time.UnixMilli(b.Timestamp).Format("02/01/2006 15:04:05"), b.Data, b.Hash)
}
```

`hex.EncodeToString` already zero-pads every byte, replacing the manual `Integer.toHexString` loop.

### `blockchain/blockchain.go`

```go
package blockchain

import (
	"strings"
	"time"
)

// Blockchain mirrors Blockchain.java.
type Blockchain struct {
	Difficulty int
	Blocks     []*Block
}

// NewBlockchain mirrors the constructor: it creates and mines the genesis block.
func NewBlockchain(difficulty int) *Blockchain {
	genesis := NewBlock(0, time.Now().UnixMilli(), "", "Bloco gênesis")
	genesis.ProofOfWork(difficulty)
	return &Blockchain{Difficulty: difficulty, Blocks: []*Block{genesis}}
}

func (bc *Blockchain) LatestBlock() *Block {
	return bc.Blocks[len(bc.Blocks)-1]
}

// NewBlock mirrors Blockchain.newBlock(String): builds the next block but does not mine
// or append it.
func (bc *Blockchain) NewBlock(data string) *Block {
	latest := bc.LatestBlock()
	return NewBlock(latest.Index+1, time.Now().UnixMilli(), latest.Hash, data)
}

// AddBlock mirrors addBlock(Block): mines the block, then appends it. Like the Java
// version it does not validate the block; IsValid is what catches bad blocks.
func (bc *Blockchain) AddBlock(b *Block) {
	if b == nil {
		return
	}
	b.ProofOfWork(bc.Difficulty)
	bc.Blocks = append(bc.Blocks, b)
}

func (bc *Blockchain) isFirstBlockValid() bool {
	first := bc.Blocks[0]
	return first.Index == 0 &&
		first.PreviousHash == "" &&
		first.Hash != "" &&
		CalculateHash(first) == first.Hash
}

func isValidNewBlock(newBlock, previousBlock *Block) bool {
	if newBlock == nil || previousBlock == nil {
		return false
	}
	return previousBlock.Index+1 == newBlock.Index &&
		newBlock.PreviousHash != "" &&
		newBlock.PreviousHash == previousBlock.Hash &&
		newBlock.Hash != "" &&
		CalculateHash(newBlock) == newBlock.Hash
}

// IsValid mirrors isBlockChainValid().
func (bc *Blockchain) IsValid() bool {
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

### `cmd/console/main.go`

```go
package main

import (
	"fmt"

	"car-rent-blockchain/blockchain"
)

func main() {
	bc := blockchain.NewBlockchain(4)
	bc.AddBlock(bc.NewBlock("Tout sur le Bitcoin"))
	bc.AddBlock(bc.NewBlock("Sylvain Saurel"))
	bc.AddBlock(bc.NewBlock("https://www.toutsurlebitcoin.fr"))
	bc.AddBlock(bc.NewBlock("https://www.uea.edu.br"))

	fmt.Println(bc)

	fmt.Println("A blockchain é válida?")
	if bc.IsValid() {
		fmt.Println("Sim, é válida!")
	} else {
		fmt.Println("Não é válida!")
	}
}
```

### `blockchain/blockchain_test.go`

```go
package blockchain

import (
	"strings"
	"testing"
)

func TestZeros(t *testing.T) {
	if got := Zeros(4); got != "0000" {
		t.Fatalf("Zeros(4) = %q", got)
	}
	if got := Zeros(0); got != "" {
		t.Fatalf("Zeros(0) = %q", got)
	}
}

// Pins the hash to the Java algorithm. Java hashes "<index+timestamp>null<data><nonce>"
// for a genesis block: 0 + 1700000000000 -> "1700000000000" + "null" + "genesis" + "0".
func TestCalculateHashMatchesJavaInput(t *testing.T) {
	b := NewBlock(0, 1700000000000, "", "genesis")
	if got, want := b.hashInput(), "1700000000000nullgenesis0"; got != want {
		t.Fatalf("hashInput = %q, want %q", got, want)
	}
	// sha256("1700000000000nullgenesis0"), computed independently with sha256sum
	const want = "819b3aedbcbd6572e5fe0c5ab48c80179a7fc6b6d931fcb4d8e7e197dc37c114"
	if b.Hash != want {
		t.Fatalf("hash = %s, want %s", b.Hash, want)
	}
}

func TestProofOfWork(t *testing.T) {
	b := NewBlock(1, 1700000000000, "abc", "data")
	b.ProofOfWork(3)
	if !strings.HasPrefix(b.Hash, "000") {
		t.Fatalf("hash %s lacks 3 leading zeros", b.Hash)
	}
	if CalculateHash(b) != b.Hash {
		t.Fatal("stored hash does not match recomputed hash")
	}
}

func newChain(t *testing.T) *Blockchain {
	t.Helper()
	bc := NewBlockchain(2)
	bc.AddBlock(bc.NewBlock("one"))
	bc.AddBlock(bc.NewBlock("two"))
	return bc
}

func TestValidChain(t *testing.T) {
	if !newChain(t).IsValid() {
		t.Fatal("fresh chain should be valid")
	}
}

func TestTamperedDataInvalidates(t *testing.T) {
	bc := newChain(t)
	bc.Blocks[1].Data = "tampered"
	if bc.IsValid() {
		t.Fatal("chain with edited data should be invalid")
	}
}

// The commented-out scenario in Main.java: a block with a bogus index and previous hash.
func TestInvalidBlockCorruptsChain(t *testing.T) {
	bc := newChain(t)
	bc.AddBlock(NewBlock(15, 1700000000000, "aaaabbb", "Block invalid"))
	if bc.IsValid() {
		t.Fatal("chain with a bogus block should be invalid")
	}
}

func TestBrokenLinkInvalidates(t *testing.T) {
	bc := newChain(t)
	bc.Blocks[2].PreviousHash = strings.Repeat("0", 64)
	if bc.IsValid() {
		t.Fatal("chain with a broken previousHash link should be invalid")
	}
}
```

The expected hash in `TestCalculateHashMatchesJavaInput` was computed with `sha256sum` on the
string the Java algorithm would build; it has not been run through the Java code itself (no JDK was
available while writing this plan). Step 5 in section 9 closes that gap.

## 7. Phase 2: the car-rental domain on top of the chain

Phase 2 adds the `rental` package. It only **uses** the `blockchain` package; it does not change it.

### 7.1 How the ledger sits on the chain

- `Block.Data` holds exactly one transaction, encoded as JSON. The Java `Block` is unchanged, so
  hash parity from phase 1 still holds.
- The genesis block keeps its plain text (`Bloco gênesis`) and is skipped when transactions are
  replayed.
- **The world state is derived from the chain.** `rental.Replay(bc) (*State, error)` rebuilds it by
  applying every transaction from block 1 in order. The server keeps a live copy that it updates as
  it accepts transactions (so it does not replay on every request), and a test asserts that the
  live state always equals a fresh replay.
- A transaction is checked against the current state **before** it is mined. If a rule fails, no
  block is created and the API answers with an error (HTTP 422 and a pt-BR message). So a valid
  chain only contains transactions that apply cleanly.
- Chain validation gets a second layer: `rental.ValidateChain(bc)` checks, block by block, the same
  things as `bc.IsValid()` (index, link, stored hash against recomputed hash) plus the proof of
  work, reports the **first** failing block with a pt-BR reason, and then replays the transactions.
  The explorer's "Validar cadeia" button calls it. While the chain is invalid, `POST /api/tx`
  answers 409 so nobody builds on a corrupted chain.

Transaction envelope:

```json
{"from": "bob", "method": "StartRental", "args": {"carId": 1, "days": 3, "deposit": 500}, "nonce": 1}
```

`nonce` must equal the sender's number of already accepted transactions plus one, which stops the
same transaction from being applied twice.

### 7.2 Accounts and roles

Fixed development accounts, created with the state (no keys, see the security note in section 8.6):

| Account | Role | pt-BR label in the UI | What the role may do |
| --- | --- | --- | --- |
| `admin` | `ADMIN` | Administrador | issue deposit tokens, settle rentals after inspecting the car |
| `alice` | `OWNER` | Proprietário | register cars and receive the rent |
| `bob` | `RENTER` | Locatário | rent cars and lock a deposit |

A transaction from an account without the required role is rejected. `escrow` is a pseudo-account
that holds locked deposits and cannot send transactions.

### 7.3 Tokens and assets

- **Deposit token.** One fungible token with integer balances per account (and `escrow`). It is
  created only by `MintDeposit`.
- **Car.** A unique asset with an `id` (counter starting at 1), `owner`, `dailyRate`,
  `minDeposit` and a status of `AVAILABLE` or `RENTED`.
- **Rental.** `id`, `carId`, `renter`, `owner`, `days`, `deposit` (locked in escrow) and a status of
  `ACTIVE` (car with the renter), `RETURNED` (renter gave it back, waiting for inspection) or
  `CLOSED` (settled).

### 7.4 Transactions and rules

| Method | Allowed role | Args | Rules and effect |
| --- | --- | --- | --- |
| `MintDeposit` | `ADMIN` | `to`, `amount` | `to` is a known account other than `escrow`; `amount > 0`; no overflow. Adds `amount` to `to`. |
| `RegisterCar` | `OWNER` | `dailyRate`, `minDeposit` | `dailyRate > 0`. Creates a car owned by the sender, `AVAILABLE`. |
| `StartRental` | `RENTER` | `carId`, `days`, `deposit` | Car exists and is `AVAILABLE`; `1 <= days <= 365`; `deposit >= minDeposit` and `deposit >= days * dailyRate`; renter balance `>= deposit`. Moves `deposit` to `escrow`, car becomes `RENTED`, rental is `ACTIVE`. |
| `ReturnCar` | `RENTER` | `rentalId` | The sender is the rental's renter and it is `ACTIVE`. Rental becomes `RETURNED`. |
| `SettleRental` | `ADMIN` | `rentalId`, `damageCharge` | Rental is `RETURNED`. `rent = days * dailyRate`. Charge `rent + damageCharge`, capped at `deposit`, goes from `escrow` to the owner; the rest goes back to the renter. Car becomes `AVAILABLE`, rental `CLOSED`. Any part of the damage that the deposit could not cover is recorded as `unpaid` in the transaction receipt. |

Every method also checks that `from` exists, that `nonce` is correct and that the arguments are
well formed (unknown fields and negative or non-integer numbers are rejected rather than read as
zero). Amounts are `uint64` with overflow checks.

Errors are Go values with an English code (for example `ErrMissingRole`, `ErrCarNotAvailable`,
`ErrInsufficientBalance`). The pt-BR text for each code lives in one lookup table, which the API
puts in its error responses.

### 7.5 Tests for phase 2

- The full happy path from section 8.7 ends with the expected balances, and total tokens are
  conserved (what was minted equals what the accounts and `escrow` hold).
- Every rule above has a rejection test, each checking that the state is unchanged afterwards.
- Role checks: an owner cannot rent, a renter cannot mint, an admin cannot register cars.
- Replaying the chain twice gives identical states.
- `ValidateChain` fails when a block's transaction data is edited, and also when the edit keeps the
  hash chain intact but makes the transaction invalid (re-mine the edited block and every block after it to build that case).

## 8. Phase 3: the web UI (Brazilian Portuguese)

### 8.1 Stack

- One static page, `web/index.html`, with `<html lang="pt-BR">`, plain HTML, CSS and JavaScript.
  No framework, no build step and no external CDN, so it also works offline.
- Embedded into the Go binary with `go:embed` and served by `cmd/server` (`go run ./cmd/server`,
  then open `http://localhost:8080`). Flags: `--difficulty` (default 3) and `--debug` (enables the
  tamper demo, section 8.4).
- The server keeps the chain in memory behind a `sync.RWMutex` (HTTP handlers run concurrently,
  mining and appending must not). The lock lives in the server, not in the `blockchain` package.

### 8.2 Acting as administrator, owner or renter

- A role selector sits in the page header, labeled **"Atuar como:"**, with the options
  **Administrador**, **Proprietário** and **Locatário**. Next to it the UI shows the active account
  (`admin`, `alice` or `bob`) and its token balance.
- Choosing an option switches the active account. From then on every transaction the UI sends has
  that account in `from`, and the tabs below change to match the role. The choice is remembered in
  `localStorage` (wrapped in try/catch so the page still works without it).
- The UI hides actions the role cannot use, but **the server is what enforces the roles**. Letting
  a role try a forbidden action on purpose (for example a renter calling `MintDeposit` from the
  browser console) and seeing the rejection is part of the lesson.

### 8.3 Tabs

| Tab (pt-BR) | Shown to | Content |
| --- | --- | --- |
| **Frota** | all roles | Every car: id, owner, daily rate, minimum deposit, status (Disponível / Alugado). |
| **Meus carros** | Proprietário | Form "Registrar carro" (diária, depósito mínimo); the owner's cars; rentals of those cars. |
| **Alugar** | Locatário | Pick an available car, enter days and deposit, see the estimated rent, then "Iniciar locação". Lists the renter's rentals with a "Devolver carro" button. |
| **Administração** | Administrador | Form "Emitir tokens de depósito" (conta, quantidade); rentals waiting for inspection, each with a damage charge field and a "Liquidar" button. |
| **Explorador da Blockchain** | all roles | Inspect the chain, see 8.4. |

Every action shows a pt-BR confirmation ("Locação iniciada no bloco #3") or the server's pt-BR error
message.

### 8.4 The "Explorador da Blockchain" tab

This tab is required, is always visible whatever the active role, and works without any
transaction having been sent (it shows the genesis block).

- **Summary bar.** Number of blocks, mining difficulty, and a validity badge: **"Cadeia válida"**
  (green) or **"Cadeia inválida: bloco #N"** (red, naming the first broken block). A
  **"Validar cadeia"** button re-runs `ValidateChain` on demand. The list refreshes by itself every
  few seconds.
- **Block list**, newest first. Each row: `#index`, date and time in pt-BR format, the first 12
  characters of `hash` with the leading zeros highlighted (the proof of work made visible), the
  first 12 of `hashAnterior`, and a one-line plain-language summary of the transaction ("bob iniciou
  a locação do carro 1 por 3 dias").
- **Block detail** (click a row). All fields: `Índice`, `Data/hora`, `Nonce`, `Hash`,
  `Hash anterior`, `Dados`. The `Dados` field shows the decoded transaction in a readable form with a
  toggle for the raw JSON. `Hash anterior` is a link to the previous block, so the chain can be
  walked backwards. A "Recalcular hash" line shows the recomputed hash next to the stored one and
  marks whether they match.
- **State panel.** Balances of `admin`, `alice`, `bob` and `escrow`, plus the cars and rentals,
  all derived from the chain.
- **Tamper demo (only with `--debug`).** On a block's detail view, a **"Adulterar dados"** button
  edits that block's `Data` in memory without re-mining it. The validity badge turns red at that
  block, and the recomputed hash no longer matches the stored one. **"Restaurar"** puts the original
  data back. This is the same corruption scenario that is commented out in `Main.java`, made
  interactive.

### 8.5 HTTP API

All paths are under `/api`, all bodies are JSON, error bodies look like
`{"codigo": "ErrMissingRole", "mensagem": "Apenas o proprietário pode registrar carros."}`.

| Endpoint | Returns |
| --- | --- |
| `GET /api/config` | `{"difficulty": N, "debug": bool}`. |
| `GET /api/blocks` | `{"difficulty": N, "blocks": [...]}`: every block with its stored fields, the recomputed hash, `hashMatches`, a pt-BR `summary` of the transaction and a `tampered` flag. |
| `GET /api/blocks/{index}` | One block, or 404. |
| `GET /api/validate` | `{"valida": true}` or `{"valida": false, "blocoInvalido": N, "motivo": "..."}`. |
| `GET /api/accounts` | Accounts with role, balance and next nonce, plus the `escrow` balance. |
| `GET /api/cars`, `GET /api/rentals` | Current cars and rentals. |
| `POST /api/tx` | Checks, mines and appends one transaction. Returns `{"block": ..., "receipt": ...}`, 422 with the error body when a rule fails, 400 for a malformed body, or 409 while the chain is invalid. |
| `POST /api/debug/tamper/{index}` and `POST /api/debug/restore/{index}` | Only with `--debug`, otherwise 404. |

### 8.6 Security note

This is a single-node study project. The role selector is **trust-based**: `from` is just a name
and requests are not signed, so anyone who can reach the server can act as anyone. The role rules
are still enforced by the server and are what the project demonstrates. Real identity needs
signatures; see section 10.

### 8.7 Acceptance walk-through

The phase is done when this runs by hand in the browser:

1. **Administrador:** emit 1000 tokens to `bob`. (block 1)
2. **Proprietário** (`alice`): register a car, diária 100, depósito mínimo 300. (block 2)
3. **Locatário** (`bob`): rent car 1 for 3 days with a deposit of 500. (block 3) `bob` has 500,
   `escrow` has 500, the car shows Alugado.
4. **Locatário:** return the car. (block 4)
5. **Administrador:** settle the rental with a damage charge of 150. (block 5) Rent is 300, so
   `alice` receives 450 and `bob` gets 50 back. Final balances: `alice` 450, `bob` 550, `escrow` 0,
   which sums to the 1000 minted. The car is Disponível again.
6. **Explorador:** six blocks (genesis plus five), badge "Cadeia válida", and the state panel
   matches step 5.
7. **Rejections:** `alice` trying to rent, `bob` trying to emit tokens, and `bob` renting a car
   that is already rented are all refused with a pt-BR message and no new block.
8. With `--debug`, **Adulterar dados** on block 2 turns the badge red at block 2, and
   **Restaurar** turns it green again.

## 9. Migration steps

### Phase 1: the port

1. Create the module and folders from section 4.
2. Port `Utils.java` to `utils.go` (no dependencies).
3. Port `Block.java` to `block.go`.
4. Port `Blockchain.java` to `blockchain.go`.
5. **Check hash parity with Java.** Run the Java example once with a fixed block, for example by
   temporarily printing `Block.calculateHash(new Block(0, 1700000000000L, null, "genesis"))`, and
   compare it with the value pinned in `TestCalculateHashMatchesJavaInput`. If they differ, the
   cause is almost always one of the two quirks in section 5.
6. Port `Main.java` to `cmd/console/main.go`.
7. Add `blockchain_test.go` with the cases from section 6, including the invalid-block scenario
   from `Main.java`.
8. Validate: `gofmt -l .`, `go vet ./...`, `go test ./...`, then `go run ./cmd/console`.

### Phase 2: the domain

9. Write `rental/tx.go` (envelope, JSON decoding with unknown fields rejected) and
   `rental/state.go` (accounts, balances, cars, rentals, `Clone`).
10. Write `rental/rules.go`: one function per method in section 7.4, plus `Replay` and
    `ValidateChain`. Each transaction runs on a copy of the state that is kept only if the rules
    pass.
11. Write `rental/rental_test.go` as listed in section 7.5. Do not start phase 3 until it passes.

### Phase 3: the web UI

12. Write `api/server.go` with the endpoints in section 8.5, and `cmd/server/main.go` with the
    `--difficulty` and `--debug` flags.
13. Build `web/index.html` in this order: the page shell with the role selector and the pt-BR `T`
    object, then the **Explorador da Blockchain** tab (it works from the first run and is the best
    way to debug the rest), then Frota, then the role tabs.
14. Run the acceptance walk-through in section 8.7 by hand, once per role switch.
15. Validate again: `gofmt -l .`, `go vet ./...`, `go test ./...`. Add a few `httptest` tests
    for the API: a role violation returns 422 and creates no block, and `/api/validate` reports
    the tampered block.

### Afterwards

16. (Once Java parity is confirmed) Harden the hash input: join the fields with a separator such as
    `|` (or hash a fixed-width or JSON encoding), use `""` for the genesis `PreviousHash` instead of
    `"null"`, and update the pinned test vector. This makes the Go version intentionally diverge
    from the Java one, so do it in its own commit.

Implementation status: all three phases are implemented in `car-rent-blockchain/`.

- `gofmt`, `go vet` and `go test -race ./...` pass (blockchain, rental rules and HTTP API tests).
- The page was exercised end to end with jsdom against a running server: role switching and tabs per
  role, the full walk-through of section 8.7, the rejection of a forged call, and the tamper and
  restore demo. That script is not kept in the repository, and no real browser was used, so the
  visual layout has not been looked at.
- Java parity (step 5) is still to be confirmed: no JDK was available.
- Not done: step 16 (hash hardening), and every item in section 10.

## 10. Optional follow-ups (study ideas)

- **Parallel mining.** Split the nonce space across goroutines, with the first hit cancelling the
  rest through `context`. Fits Go's strengths and is easy to benchmark against the sequential loop.
- **Difficulty adjustment.** Retarget difficulty so blocks take roughly constant time.
- **Real signatures.** Give each account an Ed25519 key pair (`crypto/ed25519`), sign the
  transaction envelope and verify it in the rules, so the role selector stops being trust-based.
  The old prototype in git history (`da716cb`) shows one way to do it.
- **Several transactions per block**, with a pending pool and a Merkle root in the block header
  (and a "Minerar bloco" button for the administrator).
- **Collateral items and late fees**, as in the old prototype, and a `TransferCar` method.
- **Persistence.** Write blocks to disk as JSON lines and verify the whole chain when loading.
- **Fork choice.** Accept a longer valid chain from a peer (longest-chain rule).

## 11. Repository housekeeping

The previous car-rental prototype is already deleted in the working tree (the Go node, web app,
`meta/` files and the slide deck show as deletions in `git status`, but are not committed yet).
Leftovers that no longer apply to this plan: the root `docker-compose.yml`, `README.pt-BR.md` (which
documents the deleted prototype) and a stray LibreOffice lock file,
`.~lock.car-rental-blockchain-pt-br.pptx#`. Decide whether to delete them or keep them as reference;
nothing in this plan depends on them.
