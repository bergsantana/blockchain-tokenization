package rental

import "errors"

// Error is a rule violation. Code is a stable English identifier; the pt-BR text for
// each code lives in messages_ptbr.go.
type Error struct{ Code string }

func (e *Error) Error() string { return e.Code }

var (
	ErrUnknownContract     = &Error{"ErrUnknownContract"}
	ErrUnknownAccount      = &Error{"ErrUnknownAccount"}
	ErrBadNonce            = &Error{"ErrBadNonce"}
	ErrUnknownMethod       = &Error{"ErrUnknownMethod"}
	ErrBadArgs             = &Error{"ErrBadArgs"}
	ErrMissingRole         = &Error{"ErrMissingRole"}
	ErrBadAmount           = &Error{"ErrBadAmount"}
	ErrBadRate             = &Error{"ErrBadRate"}
	ErrOverflow            = &Error{"ErrOverflow"}
	ErrCarNotFound         = &Error{"ErrCarNotFound"}
	ErrCarNotAvailable     = &Error{"ErrCarNotAvailable"}
	ErrBadDays             = &Error{"ErrBadDays"}
	ErrDepositTooLow       = &Error{"ErrDepositTooLow"}
	ErrInsufficientBalance = &Error{"ErrInsufficientBalance"}
	ErrRentalNotFound      = &Error{"ErrRentalNotFound"}
	ErrNotRenter           = &Error{"ErrNotRenter"}
	ErrWrongRentalStatus   = &Error{"ErrWrongRentalStatus"}
)

// CodeOf returns the rule-violation code inside err, or "" if err is not a rental.Error.
func CodeOf(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}
