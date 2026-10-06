package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type client struct {
	t  *testing.T
	ts *httptest.Server
}

func newClient(t *testing.T, debug bool) *client {
	t.Helper()
	ts := httptest.NewServer(New(2, debug, nil).Handler())
	t.Cleanup(ts.Close)
	return &client{t, ts}
}

func (c *client) do(method, path, body string, out any) int {
	c.t.Helper()
	req, _ := http.NewRequest(method, c.ts.URL+path, strings.NewReader(body))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			c.t.Fatalf("%s %s: bad JSON: %v", method, path, err)
		}
	}
	return resp.StatusCode
}

// tx sends a transaction, fetching the nonce the way the UI does.
func (c *client) tx(from, method, args string) (int, errorBody) {
	c.t.Helper()
	var accts struct {
		Accounts []accountView `json:"accounts"`
	}
	c.do("GET", "/api/accounts", "", &accts)
	var nonce uint64
	for _, a := range accts.Accounts {
		if a.Name == from {
			nonce = a.NextNonce
		}
	}
	body := `{"from":"` + from + `","to":"` + contractOf(method) + `","method":"` + method + `","args":` + args + `,"nonce":` + itoa(nonce) + `}`
	var e errorBody
	status := c.do("POST", "/api/tx", body, &e)
	return status, e
}

// contractOf addresses a transaction the way the web UI does.
func contractOf(method string) string {
	if method == "MintDeposit" {
		return "token"
	}
	return "rental"
}

func itoa(n uint64) string {
	b, _ := json.Marshal(n)
	return string(b)
}

func (c *client) blockCount() int {
	var out struct {
		Blocks []BlockView `json:"blocks"`
	}
	c.do("GET", "/api/blocks", "", &out)
	return len(out.Blocks)
}

func (c *client) mustTx(from, method, args string) {
	c.t.Helper()
	if status, e := c.tx(from, method, args); status != 200 {
		c.t.Fatalf("%s %s: status %d, %+v", from, method, status, e)
	}
}

func TestWalkThrough(t *testing.T) {
	c := newClient(t, false)
	if n := c.blockCount(); n != 1 {
		t.Fatalf("fresh chain has %d blocks, want 1 (genesis)", n)
	}
	c.mustTx("admin", "MintDeposit", `{"to":"bob","amount":1000}`)
	c.mustTx("alice", "RegisterCar", `{"dailyRate":100,"minDeposit":300}`)
	c.mustTx("bob", "StartRental", `{"carId":1,"days":3,"deposit":500}`)
	c.mustTx("bob", "ReturnCar", `{"rentalId":1}`)
	c.mustTx("admin", "SettleRental", `{"rentalId":1,"damageCharge":150}`)

	var accts struct {
		Accounts []accountView `json:"accounts"`
		Escrow   uint64        `json:"escrow"`
		Minted   uint64        `json:"minted"`
	}
	c.do("GET", "/api/accounts", "", &accts)
	got := map[string]uint64{}
	for _, a := range accts.Accounts {
		got[a.Name] = a.Balance
	}
	if got["alice"] != 450 || got["bob"] != 550 || got["admin"] != 0 || accts.Escrow != 0 || accts.Minted != 1000 {
		t.Fatalf("balances = %v escrow %d minted %d", got, accts.Escrow, accts.Minted)
	}
	if n := c.blockCount(); n != 6 {
		t.Fatalf("blocks = %d, want 6", n)
	}
	var v struct {
		Valid bool `json:"valida"`
	}
	c.do("GET", "/api/validate", "", &v)
	if !v.Valid {
		t.Fatal("chain should be valid")
	}
	var cars struct {
		Cars []map[string]any `json:"cars"`
	}
	c.do("GET", "/api/cars", "", &cars)
	if len(cars.Cars) != 1 || cars.Cars[0]["status"] != "AVAILABLE" {
		t.Fatalf("cars = %v", cars.Cars)
	}
}

func TestRejectedTransactionCreatesNoBlock(t *testing.T) {
	c := newClient(t, false)
	c.mustTx("admin", "MintDeposit", `{"to":"bob","amount":1000}`)
	before := c.blockCount()

	status, e := c.tx("bob", "MintDeposit", `{"to":"bob","amount":1000000}`) // renter cannot mint
	if status != http.StatusUnprocessableEntity || e.Code != "ErrMissingRole" || e.Message == "" {
		t.Fatalf("status %d body %+v", status, e)
	}
	status, e = c.tx("alice", "StartRental", `{"carId":1,"days":1,"deposit":1}`) // owner cannot rent
	if status != http.StatusUnprocessableEntity || e.Code != "ErrMissingRole" {
		t.Fatalf("status %d body %+v", status, e)
	}
	if c.blockCount() != before {
		t.Fatal("a rejected transaction created a block")
	}
}

func TestMalformedBodiesAreRefused(t *testing.T) {
	c := newClient(t, false)
	for _, body := range []string{
		`not json`,
		`{"from":"admin","to":"token","method":"MintDeposit","args":{},"nonce":1,"extra":true}`,
		`{"from":"admin","to":"token","method":"MintDeposit","args":{"to":"bob","amount":-1},"nonce":1}`,
	} {
		if status := c.do("POST", "/api/tx", body, nil); status != 400 && status != 422 {
			t.Errorf("body %q: status %d", body, status)
		}
	}
	if c.blockCount() != 1 {
		t.Fatal("malformed bodies created blocks")
	}
}

func TestTamperAndRestore(t *testing.T) {
	c := newClient(t, true)
	c.mustTx("admin", "MintDeposit", `{"to":"bob","amount":1000}`)
	c.mustTx("alice", "RegisterCar", `{"dailyRate":100,"minDeposit":300}`)

	var tampered BlockView
	edit := `{"data":"{\"from\":\"admin\",\"to\":\"token\",\"method\":\"MintDeposit\",\"args\":{\"to\":\"bob\",\"amount\":9000},\"nonce\":1}"}`
	if status := c.do("POST", "/api/debug/tamper/1", edit, &tampered); status != 200 {
		t.Fatalf("tamper status %d", status)
	}
	if tampered.HashMatches || !tampered.Tampered {
		t.Fatalf("tampered view = %+v", tampered)
	}
	var v struct {
		Valid  bool   `json:"valida"`
		Block  *int   `json:"blocoInvalido"`
		Reason string `json:"motivo"`
	}
	c.do("GET", "/api/validate", "", &v)
	if v.Valid || v.Block == nil || *v.Block != 1 || v.Reason == "" {
		t.Fatalf("validate = %+v", v)
	}
	if status, e := c.tx("bob", "ReturnCar", `{"rentalId":1}`); status != http.StatusConflict {
		t.Fatalf("tx on invalid chain: status %d %+v", status, e)
	}

	var restored BlockView
	c.do("POST", "/api/debug/restore/1", "", &restored)
	if !restored.HashMatches || restored.Tampered {
		t.Fatalf("restored view = %+v", restored)
	}
	c.do("GET", "/api/validate", "", &v)
	if !v.Valid {
		t.Fatalf("chain should be valid again: %+v", v)
	}
}

func TestDebugEndpointsAreHiddenByDefault(t *testing.T) {
	c := newClient(t, false)
	if status := c.do("POST", "/api/debug/tamper/0", `{"data":"x"}`, nil); status != 404 {
		t.Fatalf("tamper status %d, want 404", status)
	}
	if status := c.do("POST", "/api/debug/restore/0", "", nil); status != 404 {
		t.Fatalf("restore status %d, want 404", status)
	}
	var cfg struct {
		Debug bool `json:"debug"`
	}
	c.do("GET", "/api/config", "", &cfg)
	if cfg.Debug {
		t.Fatal("debug should be off")
	}
}

func TestBlockNotFound(t *testing.T) {
	c := newClient(t, false)
	if status := c.do("GET", "/api/blocks/5", "", nil); status != 404 {
		t.Fatalf("status %d", status)
	}
	if status := c.do("GET", "/api/blocks/abc", "", nil); status != 404 {
		t.Fatalf("status %d", status)
	}
	var b BlockView
	if status := c.do("GET", "/api/blocks/0", "", &b); status != 200 || b.Index != 0 || !b.HashMatches {
		t.Fatalf("genesis: status %d %+v", status, b)
	}
}
