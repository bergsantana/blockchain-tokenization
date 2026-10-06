package rental

import (
	"bytes"
	"encoding/json"
	"io"
)

// Tx is the transaction envelope stored as JSON in Block.Data. To is the address of the
// contract being called; Method is one of that contract's methods.
type Tx struct {
	From   string          `json:"from"`
	To     string          `json:"to"`
	Method string          `json:"method"`
	Args   json.RawMessage `json:"args"`
	Nonce  uint64          `json:"nonce"`
}

// decodeStrict decodes JSON rejecting unknown fields and trailing data, so malformed
// input is refused instead of being read as zero values.
func decodeStrict(data []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	if _, err := dec.Token(); err != io.EOF {
		return io.ErrUnexpectedEOF
	}
	return nil
}

// ParseTx decodes a transaction from block data.
func ParseTx(data string) (*Tx, error) {
	var tx Tx
	if err := decodeStrict([]byte(data), &tx); err != nil {
		return nil, ErrBadArgs
	}
	return &tx, nil
}

// Encode returns the compact JSON stored in Block.Data.
func (tx *Tx) Encode() (string, error) {
	b, err := json.Marshal(tx)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

type mintArgs struct {
	To     string `json:"to"`
	Amount uint64 `json:"amount"`
}

type registerCarArgs struct {
	DailyRate  uint64 `json:"dailyRate"`
	MinDeposit uint64 `json:"minDeposit"`
}

type startRentalArgs struct {
	CarID   uint64 `json:"carId"`
	Days    uint64 `json:"days"`
	Deposit uint64 `json:"deposit"`
}

type returnCarArgs struct {
	RentalID uint64 `json:"rentalId"`
}

type settleArgs struct {
	RentalID     uint64 `json:"rentalId"`
	DamageCharge uint64 `json:"damageCharge"`
}
