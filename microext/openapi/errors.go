package openapi

import "fmt"

type Error struct {
	Code        string
	Description string
}

func (e *Error) Error() string {
	return fmt.Sprintf("%s: %s", e.Code, e.Description)
}

var (
	ErrBadRequest = &Error{Code: "400", Description: "bad request"}
	ErrValidation = &Error{Code: "422", Description: "validation failed"}
	ErrInternal   = &Error{Code: "500", Description: "internal error"}
)
