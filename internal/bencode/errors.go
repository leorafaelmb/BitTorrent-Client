package bencode

import "fmt"

type DecodeError struct {
	Position int
	Reason   string
	Context  string
}

func (e *DecodeError) Error() string {
	return fmt.Sprintf("bencode decode error at position %d: %s (context %s)",
		e.Position, e.Reason, e.Context)
}
