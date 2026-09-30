// Command tx signs a transaction from the command line, for the curl
// demonstrations in Design.md section 9.
//
//	go run ./cmd/tx --key keys/bob.key --address
//	go run ./cmd/tx --key keys/bob.key --method StartRental \
//	    --args '{"CarID":1,"DaysPlanned":3,"Items":[2],"Deposit":500,"CheckoutHash":""}'
//
// Without --node it prints the signed request body, ready for
// curl $NODE/tx -d "$(go run ./cmd/tx ...)". With --node it submits the
// transaction itself and prints the node's answer.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"carrental/pkg/api"
	"carrental/pkg/chain"
	"carrental/pkg/crypto"
)

func main() {
	keyPath := flag.String("key", "", "key file of the sending account (required)")
	address := flag.Bool("address", false, "print the key's address and exit")
	method := flag.String("method", "", "contract method, e.g. StartRental")
	args := flag.String("args", "{}", "method arguments as a JSON object")
	nonce := flag.Uint64("nonce", uint64(time.Now().UnixMilli()), "transaction nonce (default: current time in ms)")
	node := flag.String("node", "", "submit to this node URL instead of printing the request body")
	flag.Parse()
	log.SetFlags(0)

	if *keyPath == "" {
		flag.Usage()
		os.Exit(2)
	}
	key, err := crypto.Load(*keyPath)
	if err != nil {
		log.Fatal(err)
	}
	if *address {
		fmt.Println(key.Address())
		return
	}
	if !json.Valid([]byte(*args)) {
		log.Fatal("--args is not valid JSON")
	}
	tx, err := chain.NewTx(key, *method, json.RawMessage(*args), *nonce)
	if err != nil {
		log.Fatal(err)
	}
	// The request body, without the tx hash that the node derives itself.
	var out any = struct {
		From      string          `json:"from"`
		Method    string          `json:"method"`
		Args      json.RawMessage `json:"args"`
		Nonce     uint64          `json:"nonce"`
		Signature string          `json:"signature"`
	}{tx.From, tx.Method, tx.Args, tx.Nonce, tx.Signature}
	if *node != "" {
		res, err := (&api.Client{Node: *node}).Submit(tx)
		var apiErr *api.Error
		if errors.As(err, &apiErr) {
			out = map[string]any{"status": apiErr.Status, "error": apiErr.Message}
		} else if err != nil {
			log.Fatal(err)
		} else {
			out = res
		}
	}
	b, _ := json.Marshal(out)
	fmt.Println(string(b))
}
