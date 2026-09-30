package chain

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"carrental/pkg/contract"
	"carrental/pkg/crypto"
)

var (
	admin = crypto.DevKey("admin")
	alice = crypto.DevKey("alice")
	bob   = crypto.DevKey("bob")
)

type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time { return c.t }

func submit(t *testing.T, c *Chain, key crypto.Key, method string, args any) *Block {
	t.Helper()
	tx, err := NewTx(key, method, args, uint64(len(c.blocks)))
	if err != nil {
		t.Fatal(err)
	}
	b, err := c.Submit(tx)
	if err != nil {
		t.Fatalf("%s: %v", method, err)
	}
	return b
}

func stateHash(c *Chain) (h crypto.Hash) {
	c.Read(func(s *contract.State) { h = s.Hash() })
	return h
}

// populate runs a full rental that is returned late, with the clock moved
// between blocks, so replay must use block times to match.
func populate(t *testing.T, c *Chain, clock *fakeClock) {
	t.Helper()
	submit(t, c, admin, "RegisterCar", map[string]any{"Owner": alice.Address(), "DailyRate": 100, "MinCollateral": 1000})
	submit(t, c, admin, "RegisterCollateral", map[string]any{"Holder": bob.Address(), "Value": 800})
	submit(t, c, admin, "MintDeposit", map[string]any{"To": bob.Address(), "Amount": 1000})
	submit(t, c, bob, "StartRental", map[string]any{"CarID": 1, "DaysPlanned": 3, "Items": []uint64{2}, "Deposit": 500})
	submit(t, c, bob, "RegisterCar", map[string]any{"Owner": bob.Address(), "DailyRate": 1, "MinCollateral": 0}) // rejected, still mined
	clock.t = clock.t.Add(7*24*time.Hour + 12*time.Hour)                                                         // 4.5 days past due: 5 started days at 150
	b := submit(t, c, admin, "SettleRental", map[string]any{"RentalID": 1})
	if fee := b.Receipts[0].Events[0].Data["lateFee"]; fee != uint64(750) {
		t.Fatalf("late fee = %v, want 750", fee)
	}
}

func newChain(t *testing.T, dir string) (*Chain, *fakeClock) {
	t.Helper()
	clock := &fakeClock{time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)}
	c, err := New(admin, dir)
	if err != nil {
		t.Fatal(err)
	}
	c.now = clock.now
	t.Cleanup(func() { c.Close() })
	return c, clock
}

func TestBlocksLinkAndSeal(t *testing.T) {
	c, clock := newChain(t, "")
	populate(t, c, clock)
	blocks := c.Blocks(100)
	if len(blocks) != 7 {
		t.Fatalf("%d blocks, want genesis + 6", len(blocks))
	}
	for i, b := range blocks {
		if b.Index != uint64(len(blocks)-1-i) {
			t.Fatalf("blocks not newest first")
		}
		if b.Hash != b.Header.hash() || !crypto.Verify(admin.Address(), b.Hash[:], b.Signature) {
			t.Errorf("block %d is not sealed correctly", b.Index)
		}
		if i+1 < len(blocks) && b.PrevHash != blocks[i+1].Hash {
			t.Errorf("block %d does not link to its parent", b.Index)
		}
	}
	if rejected := blocks[1]; rejected.Receipts[0].Error == "" || rejected.StateHash != blocks[2].StateHash {
		t.Error("a rejected transaction should be mined without changing the state")
	}
}

func TestSubmitRefusesBadSignaturesAndReplays(t *testing.T) {
	c, _ := newChain(t, "")
	tx, _ := NewTx(bob, "RegisterCar", map[string]any{}, 1)

	forged := tx
	forged.Args = json.RawMessage(`{"Owner":"x"}`)
	if _, err := c.Submit(forged); !errors.Is(err, ErrInvalidSignature) {
		t.Errorf("tampered args: err = %v", err)
	}
	garbage := tx
	garbage.Signature = "0000garbage"
	if _, err := c.Submit(garbage); !errors.Is(err, ErrInvalidSignature) {
		t.Errorf("garbage signature: err = %v", err)
	}
	if c.Tip().Index != 0 {
		t.Fatal("a refused transaction was mined")
	}
	if _, err := c.Submit(tx); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Submit(tx); !errors.Is(err, ErrDuplicateTx) {
		t.Errorf("replayed tx: err = %v", err)
	}
}

// A browser signs JSON.stringify output, which leaves "<" unescaped, while
// Go's encoder would write <. The node must keep args byte-for-byte, or
// the signature stops verifying after a restart.
func TestArgsSurviveRestartByteForByte(t *testing.T) {
	dir := t.TempDir()
	c, _ := newChain(t, dir)
	tx := Tx{From: bob.Address(), Method: "Transfer", Args: json.RawMessage(`{"TokenID": 0, "To": "<b>&</b>", "Amount": 1}`), Nonce: 7}
	tx.Args, _ = compact(tx.Args)
	tx.Signature = bob.Sign(tx.signingBytes())
	tx.Args = json.RawMessage(`{"TokenID": 0, "To": "<b>&</b>", "Amount": 1}`) // as posted, with spaces
	if _, err := c.Submit(tx); err != nil {
		t.Fatal(err)
	}
	c.Close()
	if _, err := New(admin, dir); err != nil {
		t.Fatalf("restart: %v", err)
	}
}

func TestReplayFromDisk(t *testing.T) {
	dir := t.TempDir()
	c, clock := newChain(t, dir)
	populate(t, c, clock)
	tip, state := c.Tip(), stateHash(c)
	c.Close()

	// Replaying runs days after the blocks were made; only block times count.
	again, err := New(admin, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	if again.Tip().Hash != tip.Hash || stateHash(again) != state {
		t.Fatal("replay did not reproduce the chain")
	}
	submit(t, again, admin, "MintDeposit", map[string]any{"To": alice.Address(), "Amount": 5})
}

func TestReplayRefusesTamperedLog(t *testing.T) {
	tests := []struct {
		name   string
		tamper func([][]byte) [][]byte
	}{
		{"edited args", func(lines [][]byte) [][]byte {
			lines[3] = bytes.Replace(lines[3], []byte(`"Amount":1000`), []byte(`"Amount":9000`), 1)
			return lines
		}},
		{"dropped block", func(lines [][]byte) [][]byte {
			return append(lines[:2], lines[3:]...)
		}},
		{"rewritten receipt", func(lines [][]byte) [][]byte {
			lines[5] = bytes.Replace(lines[5], []byte(`"error":"unauthorized: missing role REGISTRAR"`), []byte(`"events":[]`), 1)
			return lines
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			c, clock := newChain(t, dir)
			populate(t, c, clock)
			c.Close()

			path := filepath.Join(dir, "blocks.jsonl")
			data, _ := os.ReadFile(path)
			lines := tt.tamper(bytes.SplitAfter(data, []byte("\n")))
			edited := bytes.Join(lines, nil)
			if bytes.Equal(edited, data) {
				t.Fatal("tamper function did not change the log")
			}
			os.WriteFile(path, edited, 0o644)
			if _, err := New(admin, dir); err == nil {
				t.Fatal("tampered log was accepted")
			}
		})
	}
}

func TestReplayRefusesOtherValidator(t *testing.T) {
	dir := t.TempDir()
	c, _ := newChain(t, dir)
	c.Close()
	if _, err := New(bob, dir); err == nil {
		t.Fatal("a different validator key took over the chain")
	}
}

func compact(raw json.RawMessage) (json.RawMessage, error) {
	var buf bytes.Buffer
	err := json.Compact(&buf, raw)
	return buf.Bytes(), err
}
