// Command seed registers the demo data: alice's two cars, bob's watch, and
// bob's deposit tokens. It signs and posts transactions exactly like the web
// app does, so it doubles as a worked example of the transaction format.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"carrental/pkg/api"
	"carrental/pkg/contract"
	"carrental/pkg/crypto"
)

func main() {
	node := flag.String("node", "http://127.0.0.1:8080", "node URL")
	adminKey := flag.String("admin-key", "keys/admin.key", "key of an account with the ADMIN and REGISTRAR roles (the validator's key)")
	aliceKey := flag.String("alice-key", "", "alice's key file (default: alice.key next to --admin-key)")
	bobKey := flag.String("bob-key", "", "bob's key file (default: bob.key next to --admin-key)")
	metaDir := flag.String("meta-dir", "../meta", "directory holding the off-chain documents whose hashes go on-chain")
	flag.Parse()
	log.SetFlags(0)

	keysDir := filepath.Dir(*adminKey)
	admin := mustLoad(*adminKey)
	alice := mustLoad(orDefault(*aliceKey, filepath.Join(keysDir, "alice.key")))
	bob := mustLoad(orDefault(*bobKey, filepath.Join(keysDir, "bob.key")))
	c := &api.Client{Node: *node}

	var assets []any
	waitForNode(c, &assets)
	if len(assets) > 0 {
		fmt.Printf("chain already holds %d assets; nothing to seed\n", len(assets))
		return
	}

	rec := call(c, admin, "RegisterCar", map[string]any{
		"Owner": alice.Address(), "MetadataHash": docHash(*metaDir, "car-1.json"), "DailyRate": 100, "MinCollateral": 1000})
	fmt.Printf("car %v registered (owner alice, rate 100/day, min collateral 1000)\n", rec.Events[0].Data["carId"])

	rec = call(c, admin, "RegisterCollateral", map[string]any{
		"Holder": bob.Address(), "MetadataHash": docHash(*metaDir, "item-2.json"), "Value": 800})
	fmt.Printf("collateral item %v registered (holder bob, value 800)\n", rec.Events[0].Data["itemId"])

	rec = call(c, admin, "RegisterCar", map[string]any{
		"Owner": alice.Address(), "MetadataHash": docHash(*metaDir, "car-3.json"), "DailyRate": 50, "MinCollateral": 1200})
	fmt.Printf("car %v registered (owner alice, rate 50/day, min collateral 1200)\n", rec.Events[0].Data["carId"])

	call(c, admin, "MintDeposit", map[string]any{"To": bob.Address(), "Amount": 1000})
	fmt.Println("bob minted 1000 deposit tokens")

	fmt.Printf("\naccounts:\n  admin %s\n  alice %s\n  bob   %s\n", admin.Address(), alice.Address(), bob.Address())
}

func call(c *api.Client, key crypto.Key, method string, args any) *contract.Receipt {
	res, err := c.Call(key, method, args)
	if err != nil {
		log.Fatalf("%s: %v", method, err)
	}
	if res.Receipt.Error != "" {
		log.Fatalf("%s rejected in block %d: %s", method, res.Block, res.Receipt.Error)
	}
	return res.Receipt
}

// docHash is the SHA-256 of an off-chain document's exact bytes, which is
// what the web app recomputes to verify it.
func docHash(dir, name string) crypto.Hash {
	b, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		log.Fatalf("reading off-chain document: %v", err)
	}
	return crypto.Sum(b)
}

// waitForNode retries until the node answers, since in Docker Compose this
// container can start before the chain is listening.
func waitForNode(c *api.Client, assets *[]any) {
	deadline := time.Now().Add(30 * time.Second)
	for {
		err := c.Get("/assets", assets)
		if err == nil {
			return
		}
		if time.Now().After(deadline) {
			log.Fatalf("node %s not reachable: %v", c.Node, err)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func mustLoad(path string) crypto.Key {
	k, err := crypto.Load(path)
	if err != nil {
		log.Fatal(err)
	}
	return k
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
