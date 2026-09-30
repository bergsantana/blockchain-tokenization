package contract

import (
	"encoding/json"
	"testing"
	"time"

	"carrental/pkg/crypto"
)

var (
	admin = crypto.DevKey("admin").Address()
	alice = crypto.DevKey("alice").Address()
	bob   = crypto.DevKey("bob").Address()

	t0  = time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC) // rental start
	due = t0.Add(3 * 24 * time.Hour)
)

func call(t *testing.T, s *State, now time.Time, from, method string, args any) *Receipt {
	t.Helper()
	raw, ok := args.(string)
	if !ok {
		b, err := json.Marshal(args)
		if err != nil {
			t.Fatal(err)
		}
		raw = string(b)
	}
	return Dispatch(s, now, from, method, json.RawMessage(raw))
}

func mustCall(t *testing.T, s *State, now time.Time, from, method string, args any) *Receipt {
	t.Helper()
	rec := call(t, s, now, from, method, args)
	if rec.Error != "" {
		t.Fatalf("%s: %s", method, rec.Error)
	}
	return rec
}

// setup builds the seed data and the rental from the worked example in
// Design.md section 5: car 1 (100/day, minimum collateral 1000) owned by
// alice, bob's watch as item 2 (appraised 800), car 3 (50/day, minimum 1200),
// and bob renting car 1 for 3 days with 500 deposit tokens plus the watch.
func setup(t *testing.T) *State {
	t.Helper()
	s := NewState(admin)
	mustCall(t, s, t0, admin, "RegisterCar", map[string]any{"Owner": alice, "DailyRate": 100, "MinCollateral": 1000})
	mustCall(t, s, t0, admin, "RegisterCollateral", map[string]any{"Holder": bob, "Value": 800})
	mustCall(t, s, t0, admin, "RegisterCar", map[string]any{"Owner": alice, "DailyRate": 50, "MinCollateral": 1200})
	mustCall(t, s, t0, admin, "MintDeposit", map[string]any{"To": bob, "Amount": 1000})
	mustCall(t, s, t0, bob, "StartRental", map[string]any{"CarID": 1, "DaysPlanned": 3, "Items": []uint64{2}, "Deposit": 500})
	return s
}

func TestStartRentalLocksTokens(t *testing.T) {
	s := setup(t)
	if got := s.Balance(DepositID, bob); got != 500 {
		t.Errorf("bob deposit = %d, want 500", got)
	}
	if got := s.Balance(DepositID, Escrow); got != 500 {
		t.Errorf("escrow deposit = %d, want 500", got)
	}
	if got := s.Holder(2); got != Escrow {
		t.Errorf("watch held by %q, want escrow", got)
	}
	if !s.Assets[1].Rented || s.Holder(1) != alice {
		t.Errorf("car 1 should stay with alice and be locked")
	}
	if r := s.Rentals[1]; r.Status != StatusActive || !r.DueDate.Equal(due) || r.PrepaidRent != 300 {
		t.Errorf("rental = %+v", r)
	}
}

func TestSettlement(t *testing.T) {
	tests := []struct {
		name         string
		damage       uint64
		at           time.Time
		toOwner      uint64 // deposit tokens alice receives
		toRenter     uint64 // deposit tokens refunded to bob
		lateFee      uint64
		watchGoesTo  string
		unpaidCharge uint64
	}{
		// The three rows of the worked-example table.
		{"on time, no damage", 0, due, 300, 200, 0, bob, 0},
		{"damage charge 150", 150, due, 450, 50, 0, bob, 0},
		{"damage charge 500", 500, due, 500, 0, 0, alice, 0},
		// Late returns: each started day late costs 1.5x the daily rate.
		{"one hour late", 0, due.Add(time.Hour), 450, 50, 150, bob, 0},
		{"four days late", 0, due.Add(3*24*time.Hour + time.Hour), 500, 0, 600, alice, 0},
		{"charges beyond deposit and watch", 1500, due, 500, 0, 0, alice, 500},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := setup(t)
			rec := mustCall(t, s, tt.at, admin, "SettleRental", map[string]any{"RentalID": 1, "DamageCharge": tt.damage})

			if got := s.Balance(DepositID, alice); got != tt.toOwner {
				t.Errorf("owner received %d, want %d", got, tt.toOwner)
			}
			if got := s.Balance(DepositID, bob); got != 500+tt.toRenter {
				t.Errorf("renter holds %d, want %d", got, 500+tt.toRenter)
			}
			if got := s.Holder(2); got != tt.watchGoesTo {
				t.Errorf("watch went to %s, want %s", got, tt.watchGoesTo)
			}
			if got := s.Balance(DepositID, Escrow); got != 0 {
				t.Errorf("escrow still holds %d", got)
			}
			if s.Assets[1].Rented || s.Rentals[1].Status != StatusClosed {
				t.Error("car should be free and the rental closed")
			}
			data := rec.Events[0].Data
			if data["lateFee"] != tt.lateFee || data["unpaid"] != tt.unpaidCharge {
				t.Errorf("lateFee = %v, unpaid = %v; want %d, %d", data["lateFee"], data["unpaid"], tt.lateFee, tt.unpaidCharge)
			}
		})
	}
}

func TestRejectedCallsLeaveStateUnchanged(t *testing.T) {
	tests := []struct {
		name, from, method string
		args               any
		wantErr            string
	}{
		{"re-rent an active car", bob, "StartRental", map[string]any{"CarID": 1, "DaysPlanned": 2, "Deposit": 100}, "car unavailable"},
		{"under-collateralised", bob, "StartRental", map[string]any{"CarID": 3, "DaysPlanned": 1, "Deposit": 50}, "collateral too low"},
		{"deposit below rent", bob, "StartRental", map[string]any{"CarID": 3, "DaysPlanned": 2, "Deposit": 60}, "deposit below prepaid rent"},
		{"pledge an item in escrow", bob, "StartRental", map[string]any{"CarID": 3, "DaysPlanned": 1, "Deposit": 500, "Items": []uint64{2}}, "bad item"},
		{"deposit bob does not have", bob, "StartRental", map[string]any{"CarID": 3, "DaysPlanned": 1, "Deposit": 1300}, "insufficient deposit balance"},
		{"owner rents own car", alice, "StartRental", map[string]any{"CarID": 3, "DaysPlanned": 1, "Deposit": 1300}, "invalid request"},
		{"zero days", bob, "StartRental", map[string]any{"CarID": 3, "DaysPlanned": 0, "Deposit": 1300}, "invalid request"},
		{"renter registers a car", bob, "RegisterCar", map[string]any{"Owner": bob, "DailyRate": 100, "MinCollateral": 1000}, "unauthorized: missing role REGISTRAR"},
		{"renter settles", bob, "SettleRental", map[string]any{"RentalID": 1}, "unauthorized: missing role INSPECTOR"},
		{"renter mints", bob, "MintDeposit", map[string]any{"To": bob, "Amount": 1000}, "unauthorized: missing role ADMIN"},
		{"sell a rented car", alice, "Transfer", map[string]any{"TokenID": 1, "To": bob, "Amount": 1}, "car is rented"},
		{"move a pledged item", bob, "Transfer", map[string]any{"TokenID": 2, "To": alice, "Amount": 1}, "insufficient balance"},
		{"send tokens to escrow", bob, "Transfer", map[string]any{"TokenID": 0, "To": Escrow, "Amount": 10}, "invalid address"},
		{"settle unknown rental", admin, "SettleRental", map[string]any{"RentalID": 99}, "not active"},
		{"misspelled field", bob, "StartRental", `{"Car":3,"DaysPlanned":1,"Deposit":500}`, "bad arguments"},
		{"wrong type", bob, "StartRental", `{"CarID":"three"}`, "bad arguments"},
		{"unknown method", bob, "StealCar", `{}`, "unknown method"},
		{"sender is not an address", Escrow, "Transfer", map[string]any{"TokenID": 0, "To": bob, "Amount": 500}, "invalid sender"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := setup(t)
			before := s.Hash()
			rec := call(t, s, due, tt.from, tt.method, tt.args)
			if rec.Error != tt.wantErr {
				t.Fatalf("error = %q, want %q", rec.Error, tt.wantErr)
			}
			if s.Hash() != before {
				t.Error("a rejected call changed the state")
			}
		})
	}
}

func TestSamePledgeCannotCountTwice(t *testing.T) {
	s := setup(t)
	mustCall(t, s, t0, admin, "RegisterCollateral", map[string]any{"Holder": bob, "Value": 700}) // item 4
	before := s.Hash()
	rec := call(t, s, t0, bob, "StartRental", map[string]any{"CarID": 3, "DaysPlanned": 1, "Deposit": 50, "Items": []uint64{4, 4}})
	if rec.Error != "bad item" || s.Hash() != before {
		t.Fatalf("pledging item 4 twice: error %q", rec.Error)
	}
}

func TestCarCanBeSoldAndRentedAgainAfterReturn(t *testing.T) {
	s := setup(t)
	mustCall(t, s, due, admin, "SettleRental", map[string]any{"RentalID": 1})
	if rec := call(t, s, due, admin, "SettleRental", map[string]any{"RentalID": 1}); rec.Error != "not active" {
		t.Fatalf("second settlement: %q", rec.Error)
	}
	mustCall(t, s, due, alice, "Transfer", map[string]any{"TokenID": 1, "To": admin, "Amount": 1})
	if s.Assets[1].Owner != admin || s.Holder(1) != admin {
		t.Fatalf("car 1 owner = %s, holder = %s", s.Assets[1].Owner, s.Holder(1))
	}
	mustCall(t, s, due, bob, "StartRental", map[string]any{"CarID": 1, "DaysPlanned": 1, "Deposit": 700, "Items": []uint64{2}})
	if s.Rentals[2].CarOwner != admin {
		t.Errorf("new rental pays %s, want the new owner", s.Rentals[2].CarOwner)
	}
}

func TestCloneIsIndependent(t *testing.T) {
	s := setup(t)
	c := s.Clone()
	if c.Hash() != s.Hash() {
		t.Fatal("clone hashes differently")
	}
	mustCall(t, c, due, admin, "SettleRental", map[string]any{"RentalID": 1})
	if !s.Assets[1].Rented || s.Rentals[1].Status != StatusActive {
		t.Fatal("changing the clone changed the original")
	}
}
