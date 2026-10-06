package rental

import "encoding/json"

// Receipt reports what a transaction did, beyond the state change itself.
type Receipt struct {
	CarID    uint64 `json:"carId,omitempty"`
	RentalID uint64 `json:"rentalId,omitempty"`
	Paid     uint64 `json:"paid,omitempty"`
	Refunded uint64 `json:"refunded,omitempty"`
	Unpaid   uint64 `json:"unpaid,omitempty"`
}

// methodRoles says which role may call each method.
var methodRoles = map[string]Role{
	"MintDeposit":  RoleAdmin,
	"RegisterCar":  RoleOwner,
	"StartRental":  RoleRenter,
	"ReturnCar":    RoleRenter,
	"SettleRental": RoleAdmin,
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

func (s *State) apply(tx *Tx) (*Receipt, error) {
	role, ok := s.Roles[tx.From]
	if !ok {
		return nil, ErrUnknownAccount
	}
	if tx.Nonce != s.Nonces[tx.From]+1 {
		return nil, ErrBadNonce
	}
	required, ok := methodRoles[tx.Method]
	if !ok {
		return nil, ErrUnknownMethod
	}
	if role != required {
		return nil, ErrMissingRole
	}

	var (
		receipt *Receipt
		err     error
	)
	switch tx.Method {
	case "MintDeposit":
		receipt, err = s.mintDeposit(tx)
	case "RegisterCar":
		receipt, err = s.registerCar(tx)
	case "StartRental":
		receipt, err = s.startRental(tx)
	case "ReturnCar":
		receipt, err = s.returnCar(tx)
	case "SettleRental":
		receipt, err = s.settleRental(tx)
	}
	if err != nil {
		return nil, err
	}
	s.Nonces[tx.From]++
	return receipt, nil
}

func decodeArgs(raw json.RawMessage, v any) error {
	if len(raw) == 0 || string(raw) == "null" {
		return ErrBadArgs
	}
	if err := decodeStrict(raw, v); err != nil {
		return ErrBadArgs
	}
	return nil
}

func mulChecked(a, b uint64) (uint64, error) {
	if a != 0 && (a*b)/a != b {
		return 0, ErrOverflow
	}
	return a * b, nil
}

func addChecked(a, b uint64) (uint64, error) {
	if a+b < a {
		return 0, ErrOverflow
	}
	return a + b, nil
}

func (s *State) credit(addr string, amount uint64) error {
	v, err := addChecked(s.Balances[addr], amount)
	if err != nil {
		return err
	}
	s.Balances[addr] = v
	return nil
}

func (s *State) mintDeposit(tx *Tx) (*Receipt, error) {
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
	if err := s.credit(a.To, a.Amount); err != nil {
		return nil, err
	}
	s.Minted = minted
	return &Receipt{}, nil
}

func (s *State) registerCar(tx *Tx) (*Receipt, error) {
	var a registerCarArgs
	if err := decodeArgs(tx.Args, &a); err != nil {
		return nil, err
	}
	if a.DailyRate == 0 {
		return nil, ErrBadRate
	}
	id := s.NextCarID
	s.NextCarID++
	s.Cars[id] = &Car{ID: id, Owner: tx.From, DailyRate: a.DailyRate, MinDeposit: a.MinDeposit, Status: CarAvailable}
	return &Receipt{CarID: id}, nil
}

func (s *State) startRental(tx *Tx) (*Receipt, error) {
	var a startRentalArgs
	if err := decodeArgs(tx.Args, &a); err != nil {
		return nil, err
	}
	car, ok := s.Cars[a.CarID]
	if !ok {
		return nil, ErrCarNotFound
	}
	if car.Status != CarAvailable {
		return nil, ErrCarNotAvailable
	}
	if a.Days < 1 || a.Days > MaxDays {
		return nil, ErrBadDays
	}
	rent, err := mulChecked(a.Days, car.DailyRate)
	if err != nil {
		return nil, err
	}
	if a.Deposit < car.MinDeposit || a.Deposit < rent {
		return nil, ErrDepositTooLow
	}
	if s.Balances[tx.From] < a.Deposit {
		return nil, ErrInsufficientBalance
	}
	if err := s.credit(Escrow, a.Deposit); err != nil {
		return nil, err
	}
	s.Balances[tx.From] -= a.Deposit
	car.Status = CarRented
	id := s.NextRentalID
	s.NextRentalID++
	s.Rentals[id] = &Rental{
		ID: id, CarID: car.ID, Renter: tx.From, Owner: car.Owner,
		Days: a.Days, Rent: rent, Deposit: a.Deposit, Status: RentalActive,
	}
	return &Receipt{CarID: car.ID, RentalID: id}, nil
}

func (s *State) returnCar(tx *Tx) (*Receipt, error) {
	var a returnCarArgs
	if err := decodeArgs(tx.Args, &a); err != nil {
		return nil, err
	}
	r, ok := s.Rentals[a.RentalID]
	if !ok {
		return nil, ErrRentalNotFound
	}
	if r.Renter != tx.From {
		return nil, ErrNotRenter
	}
	if r.Status != RentalActive {
		return nil, ErrWrongRentalStatus
	}
	r.Status = RentalReturned
	return &Receipt{CarID: r.CarID, RentalID: r.ID}, nil
}

func (s *State) settleRental(tx *Tx) (*Receipt, error) {
	var a settleArgs
	if err := decodeArgs(tx.Args, &a); err != nil {
		return nil, err
	}
	r, ok := s.Rentals[a.RentalID]
	if !ok {
		return nil, ErrRentalNotFound
	}
	if r.Status != RentalReturned {
		return nil, ErrWrongRentalStatus
	}
	charge, err := addChecked(r.Rent, a.DamageCharge)
	if err != nil {
		return nil, err
	}
	var unpaid uint64
	if charge > r.Deposit {
		unpaid = charge - r.Deposit
		charge = r.Deposit
	}
	refund := r.Deposit - charge
	if err := s.credit(r.Owner, charge); err != nil {
		return nil, err
	}
	if err := s.credit(r.Renter, refund); err != nil {
		return nil, err
	}
	s.Balances[Escrow] -= r.Deposit
	r.Paid, r.Refunded, r.Unpaid = charge, refund, unpaid
	r.Status = RentalClosed
	s.Cars[r.CarID].Status = CarAvailable
	return &Receipt{CarID: r.CarID, RentalID: r.ID, Paid: charge, Refunded: refund, Unpaid: unpaid}, nil
}
