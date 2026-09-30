package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"carrental/pkg/chain"
	"carrental/pkg/crypto"
)

// Client signs and submits transactions and reads state, the same way the
// web app does. It is used by cmd/seed, cmd/tx and the tests.
type Client struct {
	Node string // base URL, e.g. http://127.0.0.1:8080

	mu        sync.Mutex
	lastNonce uint64
}

// Error is a non-2xx answer from the node.
type Error struct {
	Status  int
	Message string
}

func (e *Error) Error() string { return fmt.Sprintf("HTTP %d: %s", e.Status, e.Message) }

// Call signs a call to method with key and submits it.
func (c *Client) Call(key crypto.Key, method string, args any) (*TxResponse, error) {
	tx, err := chain.NewTx(key, method, args, c.nonce())
	if err != nil {
		return nil, err
	}
	return c.Submit(tx)
}

// Submit posts an already signed transaction.
func (c *Client) Submit(tx chain.Tx) (*TxResponse, error) {
	body, err := json.Marshal(tx)
	if err != nil {
		return nil, err
	}
	var out TxResponse
	return &out, c.do(http.MethodPost, "/tx", body, &out)
}

// Get decodes the JSON at path into out. Numbers inside untyped values (such
// as event data) decode as json.Number, so ids print as 1, not 1e+00.
func (c *Client) Get(path string, out any) error {
	return c.do(http.MethodGet, path, nil, out)
}

// nonce is the current time in milliseconds, like Date.now() in the browser,
// but never repeats within this client.
func (c *Client) nonce() uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lastNonce = max(uint64(time.Now().UnixMilli()), c.lastNonce+1)
	return c.lastNonce
}

func (c *Client) do(method, path string, body []byte, out any) error {
	req, err := http.NewRequest(method, strings.TrimSuffix(c.Node, "/")+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	dec := json.NewDecoder(resp.Body)
	dec.UseNumber()
	if resp.StatusCode/100 != 2 {
		var e struct{ Error string }
		_ = dec.Decode(&e)
		return &Error{Status: resp.StatusCode, Message: e.Error}
	}
	return dec.Decode(out)
}
