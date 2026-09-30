// Package chain turns signed transactions into a hash-linked sequence of
// blocks, each sealed by the single proof-of-authority validator.
package chain

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"carrental/pkg/contract"
	"carrental/pkg/crypto"
)

// Header is the part of a block its hash commits to. Committing to the
// receipts and the resulting state means a replay that computes anything
// different is caught at the first block where it diverges.
type Header struct {
	Index        uint64      `json:"index"`
	Timestamp    time.Time   `json:"timestamp"`
	PrevHash     crypto.Hash `json:"prevHash"`
	TxRoot       crypto.Hash `json:"txRoot"`
	ReceiptsHash crypto.Hash `json:"receiptsHash"`
	StateHash    crypto.Hash `json:"stateHash"`
	Validator    string      `json:"validator"`
}

type Block struct {
	Header
	Hash      crypto.Hash         `json:"hash"`
	Signature string              `json:"signature"` // validator's signature over Hash
	Txs       []Tx                `json:"txs"`
	Receipts  []*contract.Receipt `json:"receipts"`
}

func (h Header) hash() crypto.Hash { return hashJSON(h) }

// Chain is the node's copy of the blockchain and the state it produces.
type Chain struct {
	mu     sync.RWMutex
	key    crypto.Key
	state  *contract.State
	blocks []*Block
	seen   map[crypto.Hash]bool // hashes of every tx already in a block
	log    *os.File             // append-only block log, or nil when running in memory
	now    func() time.Time
}

// New starts a chain sealed by key, whose address is also the genesis admin.
// With an empty datadir the chain lives in memory and starts fresh. Otherwise
// blocks are appended to datadir/blocks.jsonl, and an existing log is
// verified and replayed block by block before the node accepts anything new.
func New(key crypto.Key, datadir string) (*Chain, error) {
	c := &Chain{key: key, seen: map[crypto.Hash]bool{}, now: time.Now}
	if datadir == "" {
		c.commit(c.genesis(), nil)
		return c, nil
	}
	if err := os.MkdirAll(datadir, 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(datadir, "blocks.jsonl"), os.O_RDWR|os.O_CREATE|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	c.log = f
	if err := c.replay(f); err != nil {
		f.Close()
		return nil, fmt.Errorf("%s: %w", f.Name(), err)
	}
	if len(c.blocks) == 0 {
		g := c.genesis()
		if err := c.persist(g); err != nil {
			f.Close()
			return nil, err
		}
		c.commit(g, nil)
	}
	if v := c.blocks[0].Validator; v != key.Address() {
		f.Close()
		return nil, fmt.Errorf("chain in %s is sealed by validator %s, not this node's key", datadir, v)
	}
	return c, nil
}

func (c *Chain) Close() error {
	if c.log == nil {
		return nil
	}
	return c.log.Close()
}

// Submit verifies tx, runs it through the contract in a new block, and
// returns that block. A transaction the contract rejects is still mined, with
// its error in the receipt; one that is malformed, badly signed or already on
// the chain is refused here and never reaches the contract.
func (c *Chain) Submit(tx Tx) (*Block, error) {
	if err := tx.Verify(); err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.seen[tx.Hash] {
		return nil, ErrDuplicateTx
	}
	ts := c.now().UTC().Truncate(time.Millisecond)
	if tip := c.tip(); ts.Before(tip.Timestamp) {
		ts = tip.Timestamp // block times never go backwards
	}
	b, state := c.execute(ts, []Tx{tx})
	b.Signature = c.key.Sign(b.Hash[:])
	if err := c.persist(b); err != nil {
		return nil, err
	}
	c.commit(b, state)
	return b, nil
}

// Read calls fn with the current state, which fn must not modify. A
// committed state is never mutated again (each block works on a copy), so
// values read from it stay valid after fn returns.
func (c *Chain) Read(fn func(s *contract.State)) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	fn(c.state)
}

// Blocks returns up to count blocks, newest first.
func (c *Chain) Blocks(count int) []*Block {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := []*Block{}
	for i := len(c.blocks) - 1; i >= 0 && len(out) < count; i-- {
		out = append(out, c.blocks[i])
	}
	return out
}

// Tip returns the newest block.
func (c *Chain) Tip() *Block {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.tip()
}

func (c *Chain) tip() *Block { return c.blocks[len(c.blocks)-1] }

func (c *Chain) genesis() *Block {
	b := genesisBlock(c.now().UTC().Truncate(time.Millisecond), c.key.Address())
	b.Signature = c.key.Sign(b.Hash[:])
	return b
}

// genesisBlock builds the unsigned block #0 of a chain whose validator is
// also its first admin.
func genesisBlock(ts time.Time, validator string) *Block {
	b := &Block{Header: Header{Timestamp: ts, Validator: validator}, Txs: []Tx{}, Receipts: []*contract.Receipt{}}
	b.TxRoot = txRoot(b.Txs)
	b.ReceiptsHash = hashJSON(b.Receipts)
	b.StateHash = contract.NewState(validator).Hash()
	b.Hash = b.Header.hash()
	return b
}

// execute runs verified txs on top of the tip at time ts and returns the
// unsigned block and the state after it, without committing either. Each tx
// runs on a copy of the state that is kept only if the contract accepts it.
func (c *Chain) execute(ts time.Time, txs []Tx) (*Block, *contract.State) {
	state := c.state
	receipts := make([]*contract.Receipt, 0, len(txs))
	for _, tx := range txs {
		next := state.Clone()
		rec := contract.Dispatch(next, ts, tx.From, tx.Method, tx.Args)
		if rec.Error == "" {
			state = next
		}
		receipts = append(receipts, rec)
	}
	tip := c.tip()
	b := &Block{
		Header: Header{
			Index:        tip.Index + 1,
			Timestamp:    ts,
			PrevHash:     tip.Hash,
			TxRoot:       txRoot(txs),
			ReceiptsHash: hashJSON(receipts),
			StateHash:    state.Hash(),
			Validator:    tip.Validator,
		},
		Txs:      txs,
		Receipts: receipts,
	}
	b.Hash = b.Header.hash()
	return b, state
}

func (c *Chain) commit(b *Block, state *contract.State) {
	if state == nil { // genesis
		state = contract.NewState(b.Validator)
	}
	c.state = state
	c.blocks = append(c.blocks, b)
	for _, tx := range b.Txs {
		c.seen[tx.Hash] = true
	}
}

// replay re-executes every stored block and checks that it reproduces the
// recorded hashes and carries a valid validator signature, so a log edited
// by hand, or a contract change that alters past results, is refused.
func (c *Chain) replay(r io.Reader) error {
	dec := json.NewDecoder(r)
	dec.UseNumber() // so stored receipts re-encode to exactly the bytes that were hashed
	for {
		var stored Block
		if err := dec.Decode(&stored); err == io.EOF {
			return nil
		} else if err != nil {
			return fmt.Errorf("block %d: %w", len(c.blocks), err)
		}
		if err := c.replayBlock(&stored); err != nil {
			return fmt.Errorf("block %d: %w", len(c.blocks), err)
		}
	}
}

func (c *Chain) replayBlock(stored *Block) error {
	if !crypto.Verify(stored.Validator, stored.Hash[:], stored.Signature) {
		return errors.New("bad validator signature")
	}
	if len(c.blocks) == 0 {
		g := genesisBlock(stored.Timestamp, stored.Validator)
		if g.Hash != stored.Hash {
			return errors.New("genesis block does not match")
		}
		g.Signature = stored.Signature
		c.commit(g, nil)
		return nil
	}
	tip := c.tip()
	if stored.Index != tip.Index+1 || stored.PrevHash != tip.Hash || stored.Timestamp.Before(tip.Timestamp) {
		return errors.New("does not extend the chain")
	}
	for i := range stored.Txs {
		if err := stored.Txs[i].Verify(); err != nil {
			return fmt.Errorf("tx %d: %w", i, err)
		}
		if c.seen[stored.Txs[i].Hash] {
			return fmt.Errorf("tx %d: %w", i, ErrDuplicateTx)
		}
	}
	b, state := c.execute(stored.Timestamp, stored.Txs)
	if b.StateHash != stored.StateHash {
		return fmt.Errorf("replay produced state %s, block records %s", b.StateHash, stored.StateHash)
	}
	if b.Hash != stored.Hash {
		return errors.New("replay does not reproduce the block hash")
	}
	if hashJSON(stored.Receipts) != b.ReceiptsHash {
		return errors.New("stored receipts differ from the replayed ones")
	}
	b.Signature = stored.Signature
	c.commit(b, state)
	return nil
}

func (c *Chain) persist(b *Block) error {
	if c.log == nil {
		return nil
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false) // keep args byte-for-byte as signed
	if err := enc.Encode(b); err != nil {
		return err
	}
	if _, err := c.log.Write(buf.Bytes()); err != nil {
		return err
	}
	return c.log.Sync()
}

func txRoot(txs []Tx) crypto.Hash {
	var all []byte
	for _, tx := range txs {
		all = append(all, tx.Hash[:]...)
	}
	return crypto.Sum(all)
}

func hashJSON(v any) crypto.Hash {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return crypto.Sum(b)
}
