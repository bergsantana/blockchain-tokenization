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
