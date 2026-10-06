// Package rental is the car-rental ledger that sits on top of the blockchain package.
// Each block carries one JSON transaction in Block.Data; the world state is the result of
// applying those transactions in order.
package rental

import "sort"

type Role string

const (
	RoleAdmin  Role = "ADMIN"
	RoleOwner  Role = "OWNER"
	RoleRenter Role = "RENTER"
)

// Escrow is the pseudo-account that holds locked deposits. It has no role, so it can
// neither send transactions nor receive minted tokens.
const Escrow = "escrow"

// MaxDays bounds a rental so the arithmetic stays far from overflow.
const MaxDays = 365

type CarStatus string

const (
	CarAvailable CarStatus = "AVAILABLE"
	CarRented    CarStatus = "RENTED"
)

type RentalStatus string

const (
	RentalActive   RentalStatus = "ACTIVE"   // the car is with the renter
	RentalReturned RentalStatus = "RETURNED" // given back, waiting for inspection
	RentalClosed   RentalStatus = "CLOSED"   // settled
)

type Car struct {
	ID         uint64    `json:"id"`
	Owner      string    `json:"owner"`
	DailyRate  uint64    `json:"dailyRate"`
	MinDeposit uint64    `json:"minDeposit"`
	Status     CarStatus `json:"status"`
}

type Rental struct {
	ID       uint64       `json:"id"`
	CarID    uint64       `json:"carId"`
	Renter   string       `json:"renter"`
	Owner    string       `json:"owner"`
	Days     uint64       `json:"days"`
	Rent     uint64       `json:"rent"`    // days * the car's daily rate at the time of the rental
	Deposit  uint64       `json:"deposit"` // locked in escrow
	Status   RentalStatus `json:"status"`
	Paid     uint64       `json:"paid"`     // sent to the owner when settled
	Refunded uint64       `json:"refunded"` // sent back to the renter when settled
	Unpaid   uint64       `json:"unpaid"`   // charges the deposit could not cover
}

// State is the whole world state. It is never stored on its own: it is rebuilt by
// replaying the chain (see Replay).
type State struct {
	Roles        map[string]Role
	Balances     map[string]uint64 // deposit token balances, including Escrow
	Nonces       map[string]uint64 // accepted transactions per account
	Cars         map[uint64]*Car
	Rentals      map[uint64]*Rental
	NextCarID    uint64
	NextRentalID uint64
	Minted       uint64
}

// DefaultAccounts are the fixed development accounts, in display order.
var DefaultAccounts = []struct {
	Name string
	Role Role
}{
	{"admin", RoleAdmin},
	{"alice", RoleOwner},
	{"bob", RoleRenter},
}

func NewState() *State {
	s := &State{
		Roles:        map[string]Role{},
		Balances:     map[string]uint64{},
		Nonces:       map[string]uint64{},
		Cars:         map[uint64]*Car{},
		Rentals:      map[uint64]*Rental{},
		NextCarID:    1,
		NextRentalID: 1,
	}
	for _, a := range DefaultAccounts {
		s.Roles[a.Name] = a.Role
	}
	return s
}

// Clone returns a deep copy, so a transaction can run on it and be kept or thrown away whole.
func (s *State) Clone() *State {
	c := &State{
		Roles:        make(map[string]Role, len(s.Roles)),
		Balances:     make(map[string]uint64, len(s.Balances)),
		Nonces:       make(map[string]uint64, len(s.Nonces)),
		Cars:         make(map[uint64]*Car, len(s.Cars)),
		Rentals:      make(map[uint64]*Rental, len(s.Rentals)),
		NextCarID:    s.NextCarID,
		NextRentalID: s.NextRentalID,
		Minted:       s.Minted,
	}
	for k, v := range s.Roles {
		c.Roles[k] = v
	}
	for k, v := range s.Balances {
		c.Balances[k] = v
	}
	for k, v := range s.Nonces {
		c.Nonces[k] = v
	}
	for k, v := range s.Cars {
		cp := *v
		c.Cars[k] = &cp
	}
	for k, v := range s.Rentals {
		cp := *v
		c.Rentals[k] = &cp
	}
	return c
}

// TotalHeld is the sum of every balance, escrow included. It always equals Minted.
func (s *State) TotalHeld() uint64 {
	var total uint64
	for _, v := range s.Balances {
		total += v
	}
	return total
}

// AccountNames lists the accounts in display order.
func (s *State) AccountNames() []string {
	names := make([]string, 0, len(DefaultAccounts))
	for _, a := range DefaultAccounts {
		names = append(names, a.Name)
	}
	return names
}

// CarList returns the cars ordered by id.
func (s *State) CarList() []*Car {
	out := make([]*Car, 0, len(s.Cars))
	for _, c := range s.Cars {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// RentalList returns the rentals ordered by id.
func (s *State) RentalList() []*Rental {
	out := make([]*Rental, 0, len(s.Rentals))
	for _, r := range s.Rentals {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
