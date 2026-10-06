package rental

import (
	"encoding/json"
	"fmt"
)

// Every user-facing sentence produced by this package is pt-BR and lives in this file.

var messagesPtBR = map[string]string{
	"ErrUnknownContract":     "Contrato desconhecido.",
	"ErrUnknownAccount":      "Conta desconhecida.",
	"ErrBadNonce":            "Número de sequência (nonce) incorreto. Atualize a página e tente de novo.",
	"ErrUnknownMethod":       "Operação desconhecida.",
	"ErrBadArgs":             "Argumentos inválidos ou malformados.",
	"ErrMissingRole":         "Esta conta não tem o papel necessário para esta operação.",
	"ErrBadAmount":           "A quantidade deve ser maior que zero.",
	"ErrBadRate":             "A diária deve ser maior que zero.",
	"ErrOverflow":            "Valor grande demais.",
	"ErrCarNotFound":         "Carro não encontrado.",
	"ErrCarNotAvailable":     "O carro não está disponível.",
	"ErrBadDays":             "O número de dias deve estar entre 1 e 365.",
	"ErrDepositTooLow":       "O depósito é menor que o mínimo do carro ou que o valor do aluguel.",
	"ErrInsufficientBalance": "Saldo insuficiente de tokens de depósito.",
	"ErrRentalNotFound":      "Locação não encontrada.",
	"ErrNotRenter":           "Apenas o locatário desta locação pode devolver o carro.",
	"ErrWrongRentalStatus":   "A locação não está na situação correta para esta operação.",
}

// Message returns the pt-BR text for err.
func Message(err error) string {
	if code := CodeOf(err); code != "" {
		if m, ok := messagesPtBR[code]; ok {
			return m
		}
	}
	return "Transação inválida."
}

// Summarize describes a transaction in one pt-BR sentence, for the block explorer.
// It never fails: data that is not a valid transaction gets a generic text.
func Summarize(data string) string {
	tx, err := ParseTx(data)
	if err != nil {
		return "Dados que não são uma transação válida."
	}
	switch tx.Method {
	case "MintDeposit":
		var a mintArgs
		if decodeArgs(tx.Args, &a) == nil {
			return fmt.Sprintf("%s emitiu %d tokens de depósito para %s.", tx.From, a.Amount, a.To)
		}
	case "RegisterCar":
		var a registerCarArgs
		if decodeArgs(tx.Args, &a) == nil {
			return fmt.Sprintf("%s registrou um carro (diária %d, depósito mínimo %d).", tx.From, a.DailyRate, a.MinDeposit)
		}
	case "StartRental":
		var a startRentalArgs
		if decodeArgs(tx.Args, &a) == nil {
			return fmt.Sprintf("%s iniciou a locação do carro %d por %d dias (depósito %d).", tx.From, a.CarID, a.Days, a.Deposit)
		}
	case "ReturnCar":
		var a returnCarArgs
		if decodeArgs(tx.Args, &a) == nil {
			return fmt.Sprintf("%s devolveu o carro da locação %d.", tx.From, a.RentalID)
		}
	case "SettleRental":
		var a settleArgs
		if decodeArgs(tx.Args, &a) == nil {
			return fmt.Sprintf("%s liquidou a locação %d (cobrança por danos: %d).", tx.From, a.RentalID, a.DamageCharge)
		}
	}
	raw, _ := json.Marshal(tx.Method)
	return fmt.Sprintf("Transação %s com argumentos inválidos.", raw)
}
