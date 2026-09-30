package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"carrental/pkg/chain"
	"carrental/pkg/crypto"
)

var (
	admin = crypto.DevKey("admin")
	alice = crypto.DevKey("alice")
	bob   = crypto.DevKey("bob")
)

func newNode(t *testing.T) *Client {
	t.Helper()
	bc, err := chain.New(admin, "")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(NewServer(bc))
	t.Cleanup(srv.Close)
	return &Client{Node: srv.URL}
}

func mustCall(t *testing.T, c *Client, key crypto.Key, method string, args any) *TxResponse {
	t.Helper()
	res, err := c.Call(key, method, args)
	if err != nil {
		t.Fatalf("%s: %v", method, err)
	}
	return res
}

func get[T any](t *testing.T, c *Client, path string) T {
	t.Helper()
	var v T
	if err := c.Get(path, &v); err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	return v
}

// snapshot is everything the demo shows before and after a transaction.
func snapshot(t *testing.T, c *Client) string {
	t.Helper()
	b, _ := json.Marshal([]any{
		get[any](t, c, "/assets"), get[any](t, c, "/rentals"),
		get[any](t, c, "/balance/0/"+bob.Address()), get[any](t, c, "/balance/0/escrow"),
	})
	return string(b)
}

// TestDemoScript follows Design.md section 9 step by step.
func TestDemoScript(t *testing.T) {
	c := newNode(t)

	// 9.1: seed data mines blocks 1-4.
	mustCall(t, c, admin, "RegisterCar", map[string]any{"Owner": alice.Address(), "MetadataHash": crypto.Sum([]byte("car-1")), "DailyRate": 100, "MinCollateral": 1000})
	mustCall(t, c, admin, "RegisterCollateral", map[string]any{"Holder": bob.Address(), "Value": 800})
	mustCall(t, c, admin, "RegisterCar", map[string]any{"Owner": alice.Address(), "DailyRate": 50, "MinCollateral": 1200})
	mustCall(t, c, admin, "MintDeposit", map[string]any{"To": bob.Address(), "Amount": 1000})

	// 9.3 before.
	car := get[map[string]any](t, c, "/assets/1")
	if car["kind"] != "CAR" || car["rented"] != false || car["owner"] != alice.Address() || car["metadataHash"] != crypto.Sum([]byte("car-1")).String() {
		t.Fatalf("asset 1 before = %v", car)
	}
	if got := get[json.Number](t, c, "/balance/0/"+bob.Address()); got != "1000" {
		t.Fatalf("bob before = %s", got)
	}

	// 9.2: bob starts the rental; the response names the block it is final in.
	res := mustCall(t, c, bob, "StartRental", map[string]any{"CarID": 1, "DaysPlanned": 3, "Items": []uint64{2}, "Deposit": 500, "CheckoutHash": crypto.Sum([]byte("checkout"))})
	if res.Block != 5 || res.Receipt.Error != "" || res.Receipt.Events[0].Name != "RentalStarted" {
		t.Fatalf("StartRental = %+v", res)
	}
	if blocks := get[[]map[string]any](t, c, "/blocks?count=1"); blocks[0]["hash"] == "" || blocks[0]["index"] != json.Number("5") {
		t.Fatalf("explorer tip = %v", blocks[0])
	}

	// 9.3 after.
	if car := get[map[string]any](t, c, "/assets/1"); car["rented"] != true {
		t.Fatalf("asset 1 after = %v", car)
	}
	if bal := get[json.Number](t, c, "/balance/0/"+bob.Address()); bal != "500" {
		t.Errorf("bob after = %s", bal)
	}
	if bal := get[json.Number](t, c, "/balance/0/escrow"); bal != "500" {
		t.Errorf("escrow after = %s", bal)
	}
	rental := get[map[string]any](t, c, "/rentals/1")
	if rental["status"] != "ACTIVE" || rental["renter"] != bob.Address() || rental["deposit"] != json.Number("500") {
		t.Errorf("rental 1 = %v", rental)
	}

	// 9.4: invalid operations are mined with an error and change nothing.
	before := snapshot(t, c)
	for _, tt := range []struct {
		args    map[string]any
		wantErr string
	}{
		{map[string]any{"CarID": 1, "DaysPlanned": 2, "Items": []uint64{}, "Deposit": 100}, "car unavailable"},
		{map[string]any{"CarID": 3, "DaysPlanned": 1, "Items": []uint64{}, "Deposit": 50}, "collateral too low"},
	} {
		res := mustCall(t, c, bob, "StartRental", tt.args)
		if res.Receipt.Error != tt.wantErr || res.Block == 0 {
			t.Errorf("got %+v, want %q in a block", res, tt.wantErr)
		}
	}
	if after := snapshot(t, c); after != before {
		t.Errorf("state changed:\nbefore %s\nafter  %s", before, after)
	}

	// 9.5a: a bad signature is refused at the door and never mined.
	height := get[[]map[string]any](t, c, "/blocks?count=1")[0]["index"]
	body := `{"from":"` + bob.Address() + `","method":"RegisterCar","args":{},"nonce":4,"signature":"0000garbage"}`
	resp, err := http.Post(c.Node+"/tx", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	msg, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest || !strings.Contains(string(msg), `"invalid signature"`) {
		t.Errorf("bad signature: HTTP %d %s", resp.StatusCode, msg)
	}
	if now := get[[]map[string]any](t, c, "/blocks?count=1")[0]["index"]; now != height {
		t.Errorf("height moved from %v to %v", height, now)
	}

	// 9.5b: a valid signature without the role is rejected inside the contract, but mined.
	for method, wantErr := range map[string]string{
		"RegisterCar":  "unauthorized: missing role REGISTRAR",
		"SettleRental": "unauthorized: missing role INSPECTOR",
	} {
		res := mustCall(t, c, bob, method, map[string]any{})
		if res.Receipt.Error != wantErr || res.Block == 0 {
			t.Errorf("%s: %+v", method, res)
		}
	}

	// The inspector settles on time: alice 300, bob gets 200 back and the watch.
	res = mustCall(t, c, admin, "SettleRental", map[string]any{"RentalID": 1, "DamageCharge": 0})
	if data := res.Receipt.Events[0].Data; data["paidToOwner"] != json.Number("300") || data["refundedToRenter"] != json.Number("200") {
		t.Errorf("settlement = %v", data)
	}
	if item := get[map[string]any](t, c, "/assets/2"); item["holder"] != bob.Address() {
		t.Errorf("watch holder = %v", item["holder"])
	}
}

// TestBrowserStyleTransaction posts a body the way the web app builds it:
// the signature covers JSON.stringify({from, method, args, nonce}).
func TestBrowserStyleTransaction(t *testing.T) {
	c := newNode(t)
	signed := `{"from":"` + admin.Address() + `","method":"MintDeposit","args":{"To":"` + bob.Address() + `","Amount":25},"nonce":1727712000123}`
	body := strings.TrimSuffix(signed, "}") + `,"signature":"` + admin.Sign([]byte(signed)) + `"}`
	resp, err := http.Post(c.Node+"/tx", "text/plain;charset=UTF-8", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out TxResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil || resp.StatusCode != 200 || out.Receipt.Error != "" {
		t.Fatalf("HTTP %d, %+v, %v", resp.StatusCode, out, err)
	}
	if bal := get[json.Number](t, c, "/balance/0/"+bob.Address()); bal != "25" {
		t.Errorf("bob = %s", bal)
	}

	resp2, _ := http.Post(c.Node+"/tx", "text/plain", strings.NewReader(body))
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusConflict {
		t.Errorf("replayed body: HTTP %d, want 409", resp2.StatusCode)
	}
}

func TestErrorsAndCORS(t *testing.T) {
	c := newNode(t)
	var apiErr *Error
	if err := c.Get("/assets/7", new(any)); !errors.As(err, &apiErr) || apiErr.Status != 404 {
		t.Errorf("missing asset: %v", err)
	}
	if err := c.Get("/rentals/x", new(any)); !errors.As(err, &apiErr) || apiErr.Status != 404 {
		t.Errorf("bad rental id: %v", err)
	}
	if roles := get[[]string](t, c, "/roles/"+admin.Address()); strings.Join(roles, ",") != "ADMIN,INSPECTOR,REGISTRAR" {
		t.Errorf("admin roles = %v", roles)
	}
	if roles := get[[]string](t, c, "/roles/"+bob.Address()); len(roles) != 0 {
		t.Errorf("bob roles = %v", roles)
	}

	req, _ := http.NewRequest(http.MethodOptions, c.Node+"/tx", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent || resp.Header.Get("Access-Control-Allow-Origin") != "*" {
		t.Errorf("preflight: HTTP %d %v", resp.StatusCode, resp.Header)
	}
}
