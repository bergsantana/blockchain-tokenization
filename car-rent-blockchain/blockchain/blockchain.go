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
