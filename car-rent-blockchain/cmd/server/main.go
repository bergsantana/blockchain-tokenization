// Command server starts the car-rental blockchain: the JSON API and the pt-BR web UI.
package main

import (
	"flag"
	"log"
	"net/http"
	"strings"

	"car-rent-blockchain/api"
	"car-rent-blockchain/web"
)

func main() {
	addr := flag.String("addr", ":8080", "endereço de escuta")
	difficulty := flag.Int("difficulty", 3, "dificuldade da mineração (zeros no início do hash)")
	debug := flag.Bool("debug", false, "habilita a demonstração de adulteração de blocos")
	flag.Parse()

	srv := api.New(*difficulty, *debug, web.Index())
	url := *addr
	if strings.HasPrefix(url, ":") {
		url = "localhost" + url
	}
	log.Printf("Servidor em http://%s (dificuldade %d, debug %v)", url, *difficulty, *debug)
	log.Fatal(http.ListenAndServe(*addr, srv.Handler()))
}
