package rental

import (
	"encoding/json"
	"reflect"
	"testing"

	"car-rent-blockchain/blockchain"
)

// ledger bundles a chain and the live state, the way the server does.
type ledger struct {
	bc *blockchain.Blockchain
	st *State
}

func newLedger() *ledger {
	return &ledger{bc: blockchain.NewBlockchain(2), st: NewState()}
}

// contractOf addresses a transaction the way the UI does: it asks which contract
// declares the method. Only TestContractRouting sets "to" by hand.
func contractOf(method string) string {
	for addr, c := range contracts {
		if _, ok := c.Methods()[method]; ok {
			return addr
		}
	}
	return "rental" // an unknown method: let the rental contract reject it
}

func makeTx(from, method string, nonce uint64, args any) *Tx {
	raw, err := json.Marshal(args)
	if err != nil {
		panic(err)
	}
	return &Tx{From: from, To: contractOf(method), Method: method, Args: raw, Nonce: nonce}
}

// send applies a transaction with the correct nonce and, if accepted, mines it.
func (l *ledger) send(from, method string, args any) (*Receipt, error) {
	tx := makeTx(from, method, l.st.Nonces[from]+1, args)
	rec, err := l.st.Apply(tx)
	if err != nil {
		return nil, err
	}
	data, _ := tx.Encode()
	l.bc.AddBlock(l.bc.NewBlock(data))
	return rec, nil
}

func (l *ledger) mustSend(t *testing.T, from, method string, args any) *Receipt {
	t.Helper()
	rec, err := l.send(from, method, args)
	if err != nil {
		t.Fatalf("%s by %s: %v", method, from, err)
	}
	return rec
}

type m map[string]any

// happyPath is the walk-through from the README: 5 transactions, 6 blocks.
func happyPath(t *testing.T) *ledger {
	l := newLedger()
	l.mustSend(t, "admin", "MintDeposit", m{"to": "bob", "amount": 1000})
	l.mustSend(t, "alice", "RegisterCar", m{"dailyRate": 100, "minDeposit": 300})
	l.mustSend(t, "bob", "StartRental", m{"carId": 1, "days": 3, "deposit": 500})
	l.mustSend(t, "bob", "ReturnCar", m{"rentalId": 1})
	return l
}

func TestHappyPath(t *testing.T) {
	l := happyPath(t)
	if l.st.Balances["bob"] != 500 || l.st.Balances[Escrow] != 500 || l.st.Cars[1].Status != CarRented {
		t.Fatalf("mid-rental state wrong: %+v", l.st.Balances)
	}
	rec := l.mustSend(t, "admin", "SettleRental", m{"rentalId": 1, "damageCharge": 150})
	if rec.Paid != 450 || rec.Refunded != 50 || rec.Unpaid != 0 {
		t.Fatalf("receipt = %+v", rec)
	}
	st := l.st
	if st.Balances["alice"] != 450 || st.Balances["bob"] != 550 || st.Balances[Escrow] != 0 {
		t.Fatalf("balances = %+v", st.Balances)
	}
	if st.Minted != 1000 || st.TotalHeld() != st.Minted {
		t.Fatalf("minted %d, held %d", st.Minted, st.TotalHeld())
	}
	if st.Cars[1].Status != CarAvailable || st.Rentals[1].Status != RentalClosed {
		t.Fatalf("car %v rental %v", st.Cars[1].Status, st.Rentals[1].Status)
	}
	if len(l.bc.Blocks) != 6 {
		t.Fatalf("blocks = %d, want 6", len(l.bc.Blocks))
	}
}

func TestSettleCapsAtDepositAndReportsUnpaid(t *testing.T) {
	l := happyPath(t)
	rec := l.mustSend(t, "admin", "SettleRental", m{"rentalId": 1, "damageCharge": 900})
	// rent 300 + damage 900 = 1200, deposit is only 500
	if rec.Paid != 500 || rec.Refunded != 0 || rec.Unpaid != 700 {
		t.Fatalf("receipt = %+v", rec)
	}
	if l.st.Balances["alice"] != 500 || l.st.TotalHeld() != l.st.Minted {
		t.Fatalf("balances = %+v", l.st.Balances)
	}
}

func TestRejections(t *testing.T) {
	cases := []struct {
		name   string
		from   string
		method string
		args   any
		code   string
	}{
		{"unknown account", "mallory", "MintDeposit", m{"to": "bob", "amount": 1}, "ErrUnknownAccount"},
		{"unknown method", "admin", "Burn", m{}, "ErrUnknownMethod"},
		{"renter cannot mint", "bob", "MintDeposit", m{"to": "bob", "amount": 1}, "ErrMissingRole"},
		{"owner cannot rent", "alice", "StartRental", m{"carId": 1, "days": 1, "deposit": 300}, "ErrMissingRole"},
		{"admin cannot register car", "admin", "RegisterCar", m{"dailyRate": 1, "minDeposit": 1}, "ErrMissingRole"},
		{"mint to escrow", "admin", "MintDeposit", m{"to": Escrow, "amount": 1}, "ErrUnknownAccount"},
		{"mint zero", "admin", "MintDeposit", m{"to": "bob", "amount": 0}, "ErrBadAmount"},
		{"mint overflow", "admin", "MintDeposit", m{"to": "bob", "amount": uint64(1<<64 - 1)}, "ErrOverflow"},
		{"zero rate", "alice", "RegisterCar", m{"dailyRate": 0, "minDeposit": 1}, "ErrBadRate"},
		{"unknown car", "bob", "StartRental", m{"carId": 99, "days": 1, "deposit": 300}, "ErrCarNotFound"},
		{"zero days", "bob", "StartRental", m{"carId": 1, "days": 0, "deposit": 300}, "ErrBadDays"},
		{"too many days", "bob", "StartRental", m{"carId": 1, "days": 366, "deposit": 99999}, "ErrBadDays"},
		{"deposit below minimum", "bob", "StartRental", m{"carId": 1, "days": 1, "deposit": 299}, "ErrDepositTooLow"},
		{"deposit below rent", "bob", "StartRental", m{"carId": 1, "days": 5, "deposit": 400}, "ErrDepositTooLow"},
		{"insufficient balance", "bob", "StartRental", m{"carId": 1, "days": 1, "deposit": 5000}, "ErrInsufficientBalance"},
		{"unknown rental", "bob", "ReturnCar", m{"rentalId": 7}, "ErrRentalNotFound"},
		{"settle unknown rental", "admin", "SettleRental", m{"rentalId": 7, "damageCharge": 0}, "ErrRentalNotFound"},
		{"settle before return", "admin", "SettleRental", m{"rentalId": 1, "damageCharge": 0}, "ErrRentalNotFound"},
		{"negative number", "alice", "RegisterCar", m{"dailyRate": -5, "minDeposit": 1}, "ErrBadArgs"},
		{"fractional number", "alice", "RegisterCar", m{"dailyRate": 1.5, "minDeposit": 1}, "ErrBadArgs"},
		{"unknown field", "alice", "RegisterCar", m{"dailyRate": 1, "minDeposit": 1, "extra": 1}, "ErrBadArgs"},
		{"missing args", "alice", "RegisterCar", nil, "ErrBadArgs"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			l := newLedger()
			l.mustSend(t, "admin", "MintDeposit", m{"to": "bob", "amount": 1000})
			l.mustSend(t, "alice", "RegisterCar", m{"dailyRate": 100, "minDeposit": 300})
			before, blocks := l.st.Clone(), len(l.bc.Blocks)
			_, err := l.send(c.from, c.method, c.args)
			if CodeOf(err) != c.code {
				t.Fatalf("error = %v, want %s", err, c.code)
			}
			if !reflect.DeepEqual(before, l.st) {
				t.Fatal("state changed after a rejected transaction")
			}
			if len(l.bc.Blocks) != blocks {
				t.Fatal("a block was created for a rejected transaction")
			}
			if Message(err) == "Transação inválida." {
				t.Fatalf("no pt-BR message for %s", c.code)
			}
		})
	}
}

func TestStateRulesAcrossRentalLifecycle(t *testing.T) {
	l := happyPath(t) // rental 1 is RETURNED
	for _, tc := range []struct {
		name, from, method string
		args               any
		code               string
	}{
		{"rent a rented car", "bob", "StartRental", m{"carId": 1, "days": 1, "deposit": 300}, "ErrCarNotAvailable"},
		{"return twice", "bob", "ReturnCar", m{"rentalId": 1}, "ErrWrongRentalStatus"},
	} {
		before := l.st.Clone()
		if _, err := l.send(tc.from, tc.method, tc.args); CodeOf(err) != tc.code {
			t.Fatalf("%s: error = %v, want %s", tc.name, err, tc.code)
		}
		if !reflect.DeepEqual(before, l.st) {
			t.Fatalf("%s: state changed", tc.name)
		}
	}
	l.mustSend(t, "admin", "SettleRental", m{"rentalId": 1, "damageCharge": 0})
	if _, err := l.send("admin", "SettleRental", m{"rentalId": 1, "damageCharge": 0}); CodeOf(err) != "ErrWrongRentalStatus" {
		t.Fatalf("settle twice: %v", err)
	}
}

func TestOnlyTheRenterCanReturn(t *testing.T) {
	l := newLedger()
	l.st.Roles["carla"] = RoleRenter // a second renter, added only for this test
	l.mustSend(t, "admin", "MintDeposit", m{"to": "bob", "amount": 1000})
	l.mustSend(t, "alice", "RegisterCar", m{"dailyRate": 100, "minDeposit": 300})
	l.mustSend(t, "bob", "StartRental", m{"carId": 1, "days": 1, "deposit": 300})
	if _, err := l.send("carla", "ReturnCar", m{"rentalId": 1}); CodeOf(err) != "ErrNotRenter" {
		t.Fatalf("error = %v", err)
	}
}

func TestBadNonce(t *testing.T) {
	st := NewState()
	tx := makeTx("admin", "MintDeposit", 2, m{"to": "bob", "amount": 5})
	if _, err := st.Apply(tx); CodeOf(err) != "ErrBadNonce" {
		t.Fatalf("error = %v", err)
	}
	tx.Nonce = 1
	if _, err := st.Apply(tx); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Apply(tx); CodeOf(err) != "ErrBadNonce" { // same transaction twice
		t.Fatalf("replayed tx: %v", err)
	}
}

// A transaction reaches exactly one contract: the one named in "to".
func TestContractRouting(t *testing.T) {
	mint := m{"to": "bob", "amount": 10}
	for _, c := range []struct{ to, method, code string }{
		{"token", "MintDeposit", ""},                    // the right address
		{"rental", "MintDeposit", "ErrUnknownMethod"},   // right method, wrong contract
		{"", "MintDeposit", "ErrUnknownContract"},       // no address at all
		{"escrow", "MintDeposit", "ErrUnknownContract"}, // an account is not a contract
	} {
		st := NewState()
		_, err := st.Apply(&Tx{From: "admin", To: c.to, Method: c.method, Args: mustJSON(mint), Nonce: 1})
		if CodeOf(err) != c.code {
			t.Errorf("to=%q %s: error = %v, want %q", c.to, c.method, err, c.code)
		}
	}
}

func mustJSON(v any) json.RawMessage {
	raw, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return raw
}

func TestReplayMatchesLiveState(t *testing.T) {
	l := happyPath(t)
	l.mustSend(t, "admin", "SettleRental", m{"rentalId": 1, "damageCharge": 150})
	a, err := Replay(l.bc)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := Replay(l.bc)
	if !reflect.DeepEqual(a, b) {
		t.Fatal("two replays differ")
	}
	if !reflect.DeepEqual(a, l.st) {
		t.Fatalf("replayed state differs from live state:\n%+v\n%+v", a, l.st)
	}
}

func TestValidateChain(t *testing.T) {
	l := happyPath(t)
	if v := ValidateChain(l.bc); !v.Valid {
		t.Fatalf("fresh chain invalid: %+v", v)
	}
	if !l.bc.IsValid() {
		t.Fatal("the Java-port validator disagrees on a valid chain")
	}
}

func TestEditedDataIsDetectedAtThatBlock(t *testing.T) {
	l := happyPath(t)
	l.bc.Blocks[2].Data = `{"from":"alice","to":"rental","method":"RegisterCar","args":{"dailyRate":1,"minDeposit":0},"nonce":1}`
	v := ValidateChain(l.bc)
	if v.Valid || v.InvalidBlock == nil || *v.InvalidBlock != 2 {
		t.Fatalf("validation = %+v, want invalid at block 2", v)
	}
	if l.bc.IsValid() {
		t.Fatal("the Java-port validator should also reject this chain")
	}
}

func TestBrokenLinkIsDetected(t *testing.T) {
	l := happyPath(t)
	l.bc.Blocks[3].PreviousHash = blockchain.Zeros(64)
	v := ValidateChain(l.bc)
	if v.Valid || *v.InvalidBlock != 3 {
		t.Fatalf("validation = %+v", v)
	}
}

// Editing a block and re-mining the whole chain after it keeps every hash and link valid,
// so only replaying the transactions can catch it.
func TestReminedWithInvalidTransactionIsDetectedByReplay(t *testing.T) {
	l := happyPath(t)
	// block 2 now registers a car with another owner's nonce: alice nonce 5 is out of order
	l.bc.Blocks[2].Data = `{"from":"alice","to":"rental","method":"RegisterCar","args":{"dailyRate":100,"minDeposit":300},"nonce":5}`
	for i := 2; i < len(l.bc.Blocks); i++ {
		b := l.bc.Blocks[i]
		b.PreviousHash = l.bc.Blocks[i-1].Hash
		// ProofOfWork trusts the current Hash (like the Java original), so refresh it first.
		b.Nonce = 0
		b.Hash = blockchain.CalculateHash(b)
		b.ProofOfWork(l.bc.Difficulty)
	}
	if !l.bc.IsValid() {
		t.Fatal("test setup: hashes and links should be consistent after re-mining")
	}
	v := ValidateChain(l.bc)
	if v.Valid || *v.InvalidBlock != 2 {
		t.Fatalf("validation = %+v, want invalid at block 2 by replay", v)
	}
}

func TestMissingProofOfWorkIsDetected(t *testing.T) {
	l := happyPath(t)
	last := l.bc.LatestBlock()
	for last.Nonce = 0; ; last.Nonce++ { // find a nonce whose hash does NOT meet the difficulty
		last.Hash = blockchain.CalculateHash(last)
		if last.Hash[:2] != "00" {
			break
		}
	}
	if !l.bc.IsValid() {
		t.Fatal("test setup: the Java rules do not check proof of work")
	}
	if v := ValidateChain(l.bc); v.Valid || *v.InvalidBlock != len(l.bc.Blocks)-1 {
		t.Fatalf("validation = %+v", v)
	}
}

func TestSummarize(t *testing.T) {
	l := happyPath(t)
	want := []string{
		"admin emitiu 1000 tokens de depósito para bob.",
		"alice registrou um carro (diária 100, depósito mínimo 300).",
		"bob iniciou a locação do carro 1 por 3 dias (depósito 500).",
		"bob devolveu o carro da locação 1.",
	}
	for i, w := range want {
		if got := Summarize(l.bc.Blocks[i+1].Data); got != w {
			t.Errorf("block %d: %q, want %q", i+1, got, w)
		}
	}
	if Summarize("not json") == "" {
		t.Error("Summarize must always describe the data")
	}
}
