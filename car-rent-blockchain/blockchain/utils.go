package blockchain

import "strings"

// Zeros mirrors Utils.zeros(int): a string of `length` zeros.
func Zeros(length int) string {
	return strings.Repeat("0", length)
}
