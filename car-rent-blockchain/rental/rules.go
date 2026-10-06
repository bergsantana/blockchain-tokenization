package rental

// rentalContract owns the cars and the rentals. It never touches balances directly:
// every token movement goes through the token contract.
type rentalContract struct{}

func (rentalContract) Methods() map[string]Role {
	return map[string]Role{
		"RegisterCar":  RoleOwner,
		"StartRental":  RoleRenter,
		"ReturnCar":    RoleRenter,
		"SettleRental": RoleAdmin,
	}
}

func (c rentalContract) Call(s *State, tx *Tx) (*Receipt, error) {
	switch tx.Method {
	case "RegisterCar":
		return c.registerCar(s, tx)
	case "StartRental":
		return c.startRental(s, tx)
	case "ReturnCar":
		return c.returnCar(s, tx)
	case "SettleRental":
		return c.settleRental(s, tx)
	}
	return nil, ErrUnknownMethod // unreachable: the dispatcher already checked Methods
}

func (rentalContract) registerCar(s *State, tx *Tx) (*Receipt, error) {
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

func (rentalContract) startRental(s *State, tx *Tx) (*Receipt, error) {
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
	if err := token.transfer(s, tx.From, Escrow, a.Deposit); err != nil {
		return nil, err
	}
	car.Status = CarRented
	id := s.NextRentalID
	s.NextRentalID++
	s.Rentals[id] = &Rental{
		ID: id, CarID: car.ID, Renter: tx.From, Owner: car.Owner,
		Days: a.Days, Rent: rent, Deposit: a.Deposit, Status: RentalActive,
	}
	return &Receipt{CarID: car.ID, RentalID: id}, nil
}

func (rentalContract) returnCar(s *State, tx *Tx) (*Receipt, error) {
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

func (rentalContract) settleRental(s *State, tx *Tx) (*Receipt, error) {
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
	if err := token.transfer(s, Escrow, r.Owner, charge); err != nil {
		return nil, err
	}
	if err := token.transfer(s, Escrow, r.Renter, refund); err != nil {
		return nil, err
	}
	r.Paid, r.Refunded, r.Unpaid = charge, refund, unpaid
	r.Status = RentalClosed
	s.Cars[r.CarID].Status = CarAvailable
	return &Receipt{CarID: r.CarID, RentalID: r.ID, Paid: charge, Refunded: refund, Unpaid: unpaid}, nil
}
