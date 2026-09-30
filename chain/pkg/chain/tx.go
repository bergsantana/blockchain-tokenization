package chain

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

	"carrental/pkg/crypto"
)

var (
	ErrMalformedTx      = errors.New("malformed transaction")
	ErrInvalidSignature = errors.New("invalid signature")
	ErrDuplicateTx      = errors.New("duplicate transaction")
)

// Tx is a signed call to one contract method.
type Tx struct {
	From      string          `json:"from"`
	Method    string          `json:"method"`
	Args      json.RawMessage `json:"args"`
	Nonce     uint64          `json:"nonce"`
	Signature string          `json:"signature"`
	Hash      crypto.Hash     `json:"hash"` // set by Verify; not part of what is signed
}

// NewTx builds a transaction and signs it with key.
func NewTx(key crypto.Key, method string, args any, nonce uint64) (Tx, error) {
	raw, err := json.Marshal(args)
	if err != nil {
		return Tx{}, err
	}
	tx := Tx{From: key.Address(), Method: method, Args: raw, Nonce: nonce}
	tx.Signature = key.Sign(tx.signingBytes())
	return tx, tx.Verify()
}

// Verify checks that tx is well formed and signed by its From address, and
// fills in tx.Hash.
func (tx *Tx) Verify() error {
	if !crypto.IsAddress(tx.From) || !validMethod(tx.Method) {
		return ErrMalformedTx
	}
	var args bytes.Buffer
	if err := json.Compact(&args, tx.Args); err != nil {
		return ErrMalformedTx
	}
	tx.Args = args.Bytes()
	msg := tx.signingBytes()
	if !crypto.Verify(tx.From, msg, tx.Signature) {
		return ErrInvalidSignature
	}
	tx.Hash = crypto.Sum(msg)
	return nil
}

// signingBytes is exactly what the sender signs: {"from","method","args","nonce"}
// in that order with no whitespace, which is what JSON.stringify produces for
// the same object in the browser. It expects Args already compacted, and From
// and Method already validated, so %q quotes them the way JSON would.
func (tx *Tx) signingBytes() []byte {
	return fmt.Appendf(nil, `{"from":%q,"method":%q,"args":%s,"nonce":%d}`, tx.From, tx.Method, tx.Args, tx.Nonce)
}

func validMethod(m string) bool {
	if m == "" || len(m) > 64 {
		return false
	}
	for _, c := range m {
		if !('a' <= c && c <= 'z' || 'A' <= c && c <= 'Z') {
			return false
		}
	}
	return true
}
