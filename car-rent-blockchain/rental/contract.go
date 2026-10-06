package rental

import "encoding/json"

// Contract is one program the chain can run. Contracts are compiled into the node, so
// "deploying" one means adding it to the registry below; a transaction then reaches it by
// naming its address in the "to" field.
type Contract interface {
	// Methods maps every callable method to the one role allowed to call it.
	Methods() map[string]Role
	// Call runs one method. The state it gets is already a copy, so it may change it
	// freely: the dispatcher throws that copy away if Call returns an error.
	Call(s *State, tx *Tx) (*Receipt, error)
}

// contracts is the registry: the address each contract answers at.
var contracts = map[string]Contract{
	"token":  token,
	"rental": rentalContract{},
}

// Receipt reports what a transaction did, beyond the state change itself.
type Receipt struct {
	CarID    uint64 `json:"carId,omitempty"`
	RentalID uint64 `json:"rentalId,omitempty"`
	Paid     uint64 `json:"paid,omitempty"`
	Refunded uint64 `json:"refunded,omitempty"`
	Unpaid   uint64 `json:"unpaid,omitempty"`
}

// Apply checks tx against the rules and, only if every rule passes, changes the state.
// The transaction runs on a copy, so a rejected transaction leaves s untouched.
func (s *State) Apply(tx *Tx) (*Receipt, error) {
	next := s.Clone()
	receipt, err := next.apply(tx)
	if err != nil {
		return nil, err
	}
	*s = *next
	return receipt, nil
}

// apply is the dispatcher: the checks every contract shares (caller, nonce, method, role)
// happen here, once, and only then does the contract named in tx.To get to run.
func (s *State) apply(tx *Tx) (*Receipt, error) {
	c, ok := contracts[tx.To]
	if !ok {
		return nil, ErrUnknownContract
	}
	role, ok := s.Roles[tx.From]
	if !ok {
		return nil, ErrUnknownAccount
	}
	if tx.Nonce != s.Nonces[tx.From]+1 {
		return nil, ErrBadNonce
	}
	required, ok := c.Methods()[tx.Method]
	if !ok {
		return nil, ErrUnknownMethod
	}
	if role != required {
		return nil, ErrMissingRole
	}
	receipt, err := c.Call(s, tx)
	if err != nil {
		return nil, err
	}
	s.Nonces[tx.From]++
	return receipt, nil
}

// ---- helpers every contract shares

func decodeArgs(raw json.RawMessage, v any) error {
	if len(raw) == 0 || string(raw) == "null" {
		return ErrBadArgs
	}
	if err := decodeStrict(raw, v); err != nil {
		return ErrBadArgs
	}
	return nil
}

func addChecked(a, b uint64) (uint64, error) {
	if a+b < a {
		return 0, ErrOverflow
	}
	return a + b, nil
}

func mulChecked(a, b uint64) (uint64, error) {
	if a != 0 && (a*b)/a != b {
		return 0, ErrOverflow
	}
	return a * b, nil
}
