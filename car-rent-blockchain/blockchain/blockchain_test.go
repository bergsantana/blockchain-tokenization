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
