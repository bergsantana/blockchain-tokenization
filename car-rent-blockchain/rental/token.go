package rental

// token is the deposit-token contract. It owns Balances and Minted: no other contract
// touches them except through credit and transfer below.
var token = tokenContract{}

type tokenContract struct{}

func (tokenContract) Methods() map[string]Role {
	return map[string]Role{"MintDeposit": RoleAdmin}
}

// Call mints deposit tokens. MintDeposit is the only method, so the dispatcher cannot
// send anything else here.
func (c tokenContract) Call(s *State, tx *Tx) (*Receipt, error) {
	var a mintArgs
	if err := decodeArgs(tx.Args, &a); err != nil {
		return nil, err
	}
	if _, ok := s.Roles[a.To]; !ok { // Escrow has no role, so it is refused here too
		return nil, ErrUnknownAccount
	}
	if a.Amount == 0 {
		return nil, ErrBadAmount
	}
	minted, err := addChecked(s.Minted, a.Amount)
	if err != nil {
		return nil, err
	}
	if err := c.credit(s, a.To, a.Amount); err != nil {
		return nil, err
	}
	s.Minted = minted
	return &Receipt{}, nil
}

func (tokenContract) credit(s *State, addr string, amount uint64) error {
	v, err := addChecked(s.Balances[addr], amount)
	if err != nil {
		return err
	}
	s.Balances[addr] = v
	return nil
}

// transfer moves tokens between two accounts. It is not a method, so no transaction can
// call it: it exists for the rental contract, which moves the deposit without owning the
// balances itself.
func (c tokenContract) transfer(s *State, from, to string, amount uint64) error {
	if s.Balances[from] < amount {
		return ErrInsufficientBalance
	}
	if err := c.credit(s, to, amount); err != nil {
		return err
	}
	s.Balances[from] -= amount
	return nil
}
