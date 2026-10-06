package main

import (
	"fmt"

	"car-rent-blockchain/blockchain"
)

func main() {
	bc := blockchain.NewBlockchain(4)
	bc.AddBlock(bc.NewBlock("Tout sur le Bitcoin"))
	bc.AddBlock(bc.NewBlock("Sylvain Saurel"))
	bc.AddBlock(bc.NewBlock("https://www.toutsurlebitcoin.fr"))
	bc.AddBlock(bc.NewBlock("https://www.uea.edu.br"))

	fmt.Println(bc)

	fmt.Println("A blockchain é válida?")
	if bc.IsValid() {
		fmt.Println("Sim, é válida!")
	} else {
		fmt.Println("Não é válida!")
	}
}
