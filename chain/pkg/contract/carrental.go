// Package contract is the car-rental "smart contract": the one place that
// decides whether a transaction is allowed and what it does to state. Every
// node runs this same compiled code, so every node computes the same state
// from the same sequence of transactions.
package contract

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"time"

	"carrental/pkg/crypto"
)

// DepositID is the fungible deposit token. Cars and collateral items take
// ids 1, 2, 3, ... from one shared counter.
const DepositID uint64 = 0

// Escrow is the pseudo-address that holds pledged tokens during a rental.
// It is not a valid key address, so nobody can sign for it or send to it.
const Escrow = "escrow"

// MaxRentalDays bounds DaysPlanned so the due date cannot overflow.
const MaxRentalDays = 3650

const (
	RoleAdmin     = "ADMIN"
	RoleRegistrar = "REGISTRAR"
	RoleInspector = "INSPECTOR"
)

type Kind int

const (
	KindNone Kind = iota
	KindCar
	KindCollateral
)

var kindNames = []string{"NONE", "CAR", "COLLATERAL"}

func (k Kind) MarshalJSON() ([]byte, error) { return marshalEnum(kindNames, int(k)) }
func (k *Kind) UnmarshalJSON(b []byte) error {
	v, err := unmarshalEnum(kindNames, b)
	*k = Kind(v)
	return err
}

type Status int

const (
	StatusNone Status = iota
	StatusActive
	StatusClosed
)

var statusNames = []string{"NONE", "ACTIVE", "CLOSED"}

func (s Status) MarshalJSON() ([]byte, error) { return marshalEnum(statusNames, int(s)) }
func (s *Status) UnmarshalJSON(b []byte) error {
	v, err := unmarshalEnum(statusNames, b)
	*s = Status(v)
	return err
}

type Asset struct {
	Kind         Kind        `json:"kind"`
	MetadataHash crypto.Hash `json:"metadataHash"`    // hash of the off-chain JSON (VIN, photos, papers)
	Value        uint64      `json:"value"`           // CAR: minimum collateral; COLLATERAL: appraised value
	DailyRate    uint64      `json:"dailyRate"`       // CAR only
	Rented       bool        `json:"rented"`          // CAR only
	Owner        string      `json:"owner,omitempty"` // CAR only, updated on every transfer
}

type Rental struct {
	CarID        uint64      `json:"carId"`
	Renter       string      `json:"renter"`
	CarOwner     string      `json:"carOwner"`
	Start        time.Time   `json:"start"`
	DueDate      time.Time   `json:"dueDate"`
	Deposit      uint64      `json:"deposit"` // deposit tokens locked, including prepaid rent
	PrepaidRent  uint64      `json:"prepaidRent"`
	Items        []uint64    `json:"items"`        // collateral token ids held in escrow, in pledge order
	CheckoutHash crypto.Hash `json:"checkoutHash"` // pickup condition report
	CheckinHash  crypto.Hash `json:"checkinHash"`  // return condition report
	Status       Status      `json:"status"`
}

// State is the full world state; every validator computes it identically.
type State struct {
	Ledger       map[uint64]map[string]uint64 `json:"ledger"` // tokenID -> address -> balance
	Assets       map[uint64]*Asset            `json:"assets"`
	Rentals      map[uint64]*Rental           `json:"rentals"`
	NextAssetID  uint64                       `json:"nextAssetId"`
	NextRentalID uint64                       `json:"nextRentalId"`
	Roles        map[string]map[string]bool   `json:"roles"` // role name -> address -> granted
}

func NewState(admin string) *State {
	return &State{
		Ledger: map[uint64]map[string]uint64{}, Assets: map[uint64]*Asset{}, Rentals: map[uint64]*Rental{},
		NextAssetID: 1, NextRentalID: 1,
		Roles: map[string]map[string]bool{RoleRegistrar: {admin: true}, RoleInspector: {admin: true}, RoleAdmin: {admin: true}},
	}
}

// Hash commits to the whole state. encoding/json writes map keys in sorted
// order, so equal states always produce equal bytes.
func (s *State) Hash() crypto.Hash {
	b, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return crypto.Sum(b)
}

// Clone returns a deep copy, so a transaction can run against it and be
// committed or thrown away as a whole.
func (s *State) Clone() *State {
	b, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	c := &State{}
	if err := json.Unmarshal(b, c); err != nil {
		panic(err)
	}
	return c
}

// Balance returns how many units of token id addr holds.
func (s *State) Balance(id uint64, addr string) uint64 { return s.Ledger[id][addr] }

// Holder returns who holds a car or collateral token (possibly Escrow).
func (s *State) Holder(id uint64) string {
	// Zero balances are deleted, so a supply-1 token has exactly one entry.
	for addr := range s.Ledger[id] {
		return addr
	}
	return ""
}

// RolesOf returns the roles granted to addr, sorted.
func (s *State) RolesOf(addr string) []string {
	roles := []string{}
	for role, holders := range s.Roles {
		if holders[addr] {
			roles = append(roles, role)
		}
	}
	slices.Sort(roles)
	return roles
}

func (s *State) require(role, addr string) error {
	if !s.Roles[role][addr] {
		return fmt.Errorf("unauthorized: missing role %s", role)
	}
	return nil
}

func (s *State) setBalance(id uint64, addr string, v uint64) {
	if v == 0 {
		delete(s.Ledger[id], addr)
		if len(s.Ledger[id]) == 0 {
			delete(s.Ledger, id)
		}
		return
	}
	if s.Ledger[id] == nil {
		s.Ledger[id] = map[string]uint64{}
	}
	s.Ledger[id][addr] = v
}

func (s *State) mint(id uint64, to string, amount uint64) {
	s.setBalance(id, to, s.Balance(id, to)+amount)
}

// move transfers tokens between addresses. Handlers check balances before
// their first mutation, so a shortfall here is a contract bug, and panicking
// is safer than committing a half-applied transaction.
func (s *State) move(id uint64, from, to string, amount uint64) {
	if s.Balance(id, from) < amount {
		panic(fmt.Sprintf("contract bug: moving %d of token %d from %s exceeds its balance", amount, id, from))
	}
	s.setBalance(id, from, s.Balance(id, from)-amount)
	s.mint(id, to, amount)
}

// supply sums every balance of a token. Map order does not matter for a sum.
func (s *State) supply(id uint64) uint64 {
	var total uint64
	for _, v := range s.Ledger[id] {
		total += v
	}
	return total
}

// Event mirrors a Solidity event: recorded in the block receipt for the explorer tab.
type Event struct {
	Name string         `json:"name"`
	Data map[string]any `json:"data"`
}

type Receipt struct {
	Events []Event `json:"events,omitempty"`
	Error  string  `json:"error,omitempty"`
}

var (
	errBadArgs        = errors.New("bad arguments")
	errInvalidAddress = errors.New("invalid address")
	errInvalidAmount  = errors.New("invalid amount")
	errInvalidRequest = errors.New("invalid request")
)

// Dispatch is the single entry point every node calls for every transaction.
// now is the block's timestamp rather than the local clock, so every node
// replaying the chain computes the same due dates and late fees. Each handler
// validates completely before its first mutation, so a rejected transaction
// leaves state exactly as it was.
func Dispatch(s *State, now time.Time, from, method string, args json.RawMessage) *Receipt {
	var events []Event
	var err error
	if !crypto.IsAddress(from) {
		err = errors.New("invalid sender")
	} else {
		switch method {
		case "MintDeposit":
			events, err = s.mintDeposit(from, args)
		case "RegisterCar":
			events, err = s.registerCar(from, args)
		case "RegisterCollateral":
			events, err = s.registerCollateral(from, args)
		case "Transfer":
			events, err = s.transfer(from, args)
		case "StartRental":
			events, err = s.startRental(now, from, args)
		case "SettleRental":
			events, err = s.settleRental(now, from, args)
		default:
			err = errors.New("unknown method")
		}
	}
	if err != nil {
		return &Receipt{Error: err.Error()}
	}
	return &Receipt{Events: events}
}

// decodeArgs rejects unknown fields so a typo cannot silently become a zero
// value. The error text is fixed because receipts are hashed into blocks, and
// encoding/json's messages may change between Go versions.
func decodeArgs(raw json.RawMessage, v any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return errBadArgs
	}
	return nil
}

func (s *State) mintDeposit(from string, raw json.RawMessage) ([]Event, error) {
	if err := s.require(RoleAdmin, from); err != nil {
		return nil, err
	}
	var a struct {
		To     string
		Amount uint64
	}
	if err := decodeArgs(raw, &a); err != nil {
		return nil, err
	}
	if !crypto.IsAddress(a.To) {
		return nil, errInvalidAddress
	}
	// Capping total supply keeps every balance, including escrow's, from overflowing.
	if a.Amount == 0 || s.supply(DepositID) > math.MaxUint64-a.Amount {
		return nil, errInvalidAmount
	}
	s.mint(DepositID, a.To, a.Amount)
	return []Event{{"DepositMinted", map[string]any{"to": a.To, "amount": a.Amount}}}, nil
}

func (s *State) registerCar(from string, raw json.RawMessage) ([]Event, error) {
	if err := s.require(RoleRegistrar, from); err != nil {
		return nil, err
	}
	var a struct {
		Owner         string
		MetadataHash  crypto.Hash
		DailyRate     uint64
		MinCollateral uint64
	}
	if err := decodeArgs(raw, &a); err != nil {
		return nil, err
	}
	if !crypto.IsAddress(a.Owner) {
		return nil, errInvalidAddress
	}
	if a.DailyRate == 0 {
		return nil, errors.New("invalid daily rate")
	}
	id := s.NextAssetID
	s.NextAssetID++
	s.Assets[id] = &Asset{Kind: KindCar, MetadataHash: a.MetadataHash, Value: a.MinCollateral, DailyRate: a.DailyRate, Owner: a.Owner}
	s.mint(id, a.Owner, 1)
	return []Event{{"CarRegistered", map[string]any{
		"carId": id, "owner": a.Owner, "dailyRate": a.DailyRate, "minCollateral": a.MinCollateral}}}, nil
}

func (s *State) registerCollateral(from string, raw json.RawMessage) ([]Event, error) {
	if err := s.require(RoleRegistrar, from); err != nil {
		return nil, err
	}
	var a struct {
		Holder       string
		MetadataHash crypto.Hash
		Value        uint64
	}
	if err := decodeArgs(raw, &a); err != nil {
		return nil, err
	}
	if !crypto.IsAddress(a.Holder) {
		return nil, errInvalidAddress
	}
	if a.Value == 0 {
		return nil, errors.New("invalid value")
	}
	id := s.NextAssetID
	s.NextAssetID++
	s.Assets[id] = &Asset{Kind: KindCollateral, MetadataHash: a.MetadataHash, Value: a.Value}
	s.mint(id, a.Holder, 1)
	return []Event{{"CollateralRegistered", map[string]any{"itemId": id, "holder": a.Holder, "value": a.Value}}}, nil
}

// transfer moves deposit units, a car or a collateral item to another
// account. A rented car cannot move, and an item in escrow is no longer the
// sender's to move.
func (s *State) transfer(from string, raw json.RawMessage) ([]Event, error) {
	var a struct {
		TokenID uint64
		To      string
		Amount  uint64
	}
	if err := decodeArgs(raw, &a); err != nil {
		return nil, err
	}
	if !crypto.IsAddress(a.To) {
		return nil, errInvalidAddress
	}
	if a.To == from {
		return nil, errInvalidRequest
	}
	asset := s.Assets[a.TokenID]
	switch {
	case a.TokenID == DepositID:
		if a.Amount == 0 {
			return nil, errInvalidAmount
		}
	case asset == nil:
		return nil, errors.New("unknown token")
	case asset.Rented:
		return nil, errors.New("car is rented")
	case a.Amount != 1:
		return nil, errInvalidAmount
	}
	if s.Balance(a.TokenID, from) < a.Amount {
		return nil, errors.New("insufficient balance")
	}
	s.move(a.TokenID, from, a.To, a.Amount)
	if asset != nil && asset.Kind == KindCar {
		asset.Owner = a.To
	}
	return []Event{{"Transfer", map[string]any{"tokenId": a.TokenID, "from": from, "to": a.To, "amount": a.Amount}}}, nil
}

func (s *State) startRental(now time.Time, from string, raw json.RawMessage) ([]Event, error) {
	var a struct {
		CarID        uint64
		DaysPlanned  uint64
		Items        []uint64
		Deposit      uint64
		CheckoutHash crypto.Hash
	}
	if err := decodeArgs(raw, &a); err != nil {
		return nil, err
	}
	car := s.Assets[a.CarID]
	if car == nil || car.Kind != KindCar || car.Rented {
		return nil, errors.New("car unavailable")
	}
	if car.Owner == from || a.DaysPlanned == 0 || a.DaysPlanned > MaxRentalDays {
		return nil, errInvalidRequest
	}
	prepaid := car.DailyRate * a.DaysPlanned
	if prepaid/a.DaysPlanned != car.DailyRate { // overflowed
		return nil, errInvalidRequest
	}
	if a.Deposit < prepaid {
		return nil, errors.New("deposit below prepaid rent")
	}
	cover := a.Deposit - prepaid
	pledged := map[uint64]bool{}
	for _, itemID := range a.Items {
		it := s.Assets[itemID]
		// Pledging the same item twice would count its value twice.
		if it == nil || it.Kind != KindCollateral || s.Balance(itemID, from) != 1 || pledged[itemID] {
			return nil, errors.New("bad item")
		}
		pledged[itemID] = true
		cover = satAdd(cover, it.Value)
	}
	if cover < car.Value {
		return nil, errors.New("collateral too low")
	}
	if s.Balance(DepositID, from) < a.Deposit {
		return nil, errors.New("insufficient deposit balance")
	}

	items := append([]uint64{}, a.Items...)
	id := s.NextRentalID
	s.NextRentalID++
	r := &Rental{CarID: a.CarID, Renter: from, CarOwner: car.Owner, Start: now,
		DueDate: now.Add(time.Duration(a.DaysPlanned) * 24 * time.Hour),
		Deposit: a.Deposit, PrepaidRent: prepaid, Items: items, CheckoutHash: a.CheckoutHash, Status: StatusActive}
	s.Rentals[id] = r
	car.Rented = true

	s.move(DepositID, from, Escrow, a.Deposit)
	for _, itemID := range items {
		s.move(itemID, from, Escrow, 1)
	}
	return []Event{{"RentalStarted", map[string]any{
		"rentalId": id, "carId": a.CarID, "renter": from, "dueDate": r.DueDate, "deposit": a.Deposit, "items": items}}}, nil
}

func (s *State) settleRental(now time.Time, from string, raw json.RawMessage) ([]Event, error) {
	if err := s.require(RoleInspector, from); err != nil {
		return nil, err
	}
	var a struct {
		RentalID     uint64
		DamageCharge uint64
		CheckinHash  crypto.Hash
	}
	if err := decodeArgs(raw, &a); err != nil {
		return nil, err
	}
	r := s.Rentals[a.RentalID]
	if r == nil || r.Status != StatusActive {
		return nil, errors.New("not active")
	}
	car := s.Assets[r.CarID]

	// Work out the whole settlement first; nothing below can fail.
	var lateFee uint64
	if now.After(r.DueDate) {
		lateDays := uint64(now.Sub(r.DueDate).Hours()/24) + 1
		lateFee = satMul(lateDays, car.DailyRate)
		lateFee = satMul(lateFee, 3) / 2
	}
	owed := satAdd(lateFee, a.DamageCharge)
	spare := r.Deposit - r.PrepaidRent
	fromDeposit := min(owed, spare)
	toOwner := r.PrepaidRent + fromDeposit
	refund := spare - fromDeposit
	shortfall := owed - fromDeposit

	seized, returned := []uint64{}, []uint64{}
	for _, itemID := range r.Items { // order = pledge order, never map iteration
		if shortfall == 0 {
			returned = append(returned, itemID)
			continue
		}
		seized = append(seized, itemID)
		shortfall -= min(shortfall, s.Assets[itemID].Value)
	}

	r.Status = StatusClosed
	r.CheckinHash = a.CheckinHash
	car.Rented = false
	for _, itemID := range seized {
		s.move(itemID, Escrow, r.CarOwner, 1)
	}
	for _, itemID := range returned {
		s.move(itemID, Escrow, r.Renter, 1)
	}
	s.move(DepositID, Escrow, r.CarOwner, toOwner)
	s.move(DepositID, Escrow, r.Renter, refund)
	return []Event{{"RentalSettled", map[string]any{
		"rentalId": a.RentalID, "paidToOwner": toOwner, "refundedToRenter": refund,
		"lateFee": lateFee, "damageCharge": a.DamageCharge, "seized": seized,
		"unpaid": shortfall}}}, nil // charges left over after every item was seized
}

func satAdd(a, b uint64) uint64 {
	if a > math.MaxUint64-b {
		return math.MaxUint64
	}
	return a + b
}

func satMul(a, b uint64) uint64 {
	if a != 0 && b > math.MaxUint64/a {
		return math.MaxUint64
	}
	return a * b
}

func marshalEnum(names []string, v int) ([]byte, error) {
	if v < 0 || v >= len(names) {
		return nil, fmt.Errorf("enum value %d out of range", v)
	}
	return json.Marshal(names[v])
}

func unmarshalEnum(names []string, b []byte) (int, error) {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return 0, err
	}
	if i := slices.Index(names, s); i >= 0 {
		return i, nil
	}
	return 0, fmt.Errorf("unknown enum value %q", s)
}
