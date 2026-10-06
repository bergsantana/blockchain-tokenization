package rental

import (
	"fmt"
	"strings"

	"car-rent-blockchain/blockchain"
)

// ReplayError says which block carried a transaction that does not apply.
type ReplayError struct {
	Block int
	Err   error
}

func (e *ReplayError) Error() string { return fmt.Sprintf("block %d: %v", e.Block, e.Err) }
func (e *ReplayError) Unwrap() error { return e.Err }

// Replay rebuilds the world state by applying every transaction, in block order.
// Block 0 (the genesis block) holds plain text and is skipped.
func Replay(bc *blockchain.Blockchain) (*State, error) {
	s := NewState()
	for _, b := range bc.Blocks[1:] {
		tx, err := ParseTx(b.Data)
		if err != nil {
			return nil, &ReplayError{b.Index, err}
		}
		if _, err := s.Apply(tx); err != nil {
			return nil, &ReplayError{b.Index, err}
		}
	}
	return s, nil
}

// Validation is the result of ValidateChain.
type Validation struct {
	Valid        bool   `json:"valida"`
	InvalidBlock *int   `json:"blocoInvalido,omitempty"`
	Reason       string `json:"motivo,omitempty"`
}

func invalid(block int, reason string) Validation {
	return Validation{Valid: false, InvalidBlock: &block, Reason: reason}
}

// ValidateChain checks, block by block, the same things Blockchain.IsValid checks (index,
// link to the previous block, stored hash against recomputed hash), plus the proof of
// work, and then replays the transactions. It reports the first block that fails.
func ValidateChain(bc *blockchain.Blockchain) Validation {
	target := blockchain.Zeros(bc.Difficulty)
	for i, b := range bc.Blocks {
		switch {
		case b.Index != i:
			return invalid(i, "O índice do bloco está incorreto.")
		case i == 0 && b.PreviousHash != "":
			return invalid(i, "O bloco gênesis não pode ter hash anterior.")
		case i > 0 && b.PreviousHash != bc.Blocks[i-1].Hash:
			return invalid(i, "O hash anterior não confere com o hash do bloco anterior.")
		case b.Hash == "" || blockchain.CalculateHash(b) != b.Hash:
			return invalid(i, "O hash armazenado difere do hash recalculado: os dados do bloco foram alterados.")
		case !strings.HasPrefix(b.Hash, target):
			return invalid(i, "O hash não atende à dificuldade (prova de trabalho ausente).")
		}
	}
	if _, err := Replay(bc); err != nil {
		if re, ok := err.(*ReplayError); ok {
			return invalid(re.Block, "A transação deste bloco não é válida: "+Message(re.Err))
		}
		return invalid(0, Message(err))
	}
	return Validation{Valid: true}
}
