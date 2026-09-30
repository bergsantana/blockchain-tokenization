// Command node runs the car-rental blockchain: one validator that seals a
// block for every transaction it receives over HTTP.
package main

import (
	"flag"
	"log"
	"net/http"
	"strings"
	"time"

	"carrental/pkg/api"
	"carrental/pkg/chain"
	"carrental/pkg/crypto"
)

func main() {
	keyPath := flag.String("validator-key", "keys/validator.key", "validator key file, created if missing; this account is also the chain's admin")
	addr := flag.String("addr", ":8080", "HTTP listen address")
	datadir := flag.String("datadir", "", "directory to persist blocks in (default: in memory, a fresh chain on every start)")
	flag.Parse()
	log.SetFlags(log.Ltime)

	key, err := crypto.LoadOrCreate(*keyPath)
	if err != nil {
		log.Fatal(err)
	}
	bc, err := chain.New(key, *datadir)
	if err != nil {
		log.Fatal(err)
	}
	defer bc.Close()

	if tip := bc.Tip(); tip.Index == 0 {
		log.Printf("genesis block #0 committed, hash %s", tip.Hash)
	} else {
		log.Printf("replayed and verified %d blocks from %s, tip #%d hash %s", tip.Index+1, *datadir, tip.Index, tip.Hash)
	}
	log.Printf("validator and admin: %s", key.Address())

	host := *addr
	if strings.HasPrefix(host, ":") {
		host = "127.0.0.1" + host
	}
	log.Printf("blockchain node listening on http://%s", host)
	srv := &http.Server{Addr: *addr, Handler: api.NewServer(bc), ReadHeaderTimeout: 5 * time.Second}
	log.Fatal(srv.ListenAndServe())
}
