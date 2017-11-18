package httperr

import (
	"fmt"
)

type Error struct {
	Code    int
	Message string
}

func (e Error) String() string {
	return fmt.Sprintf("code: %v, message: %v", e.Code, e.Message)
}

var UnAuthorized = Error{Code: 401, Message: "unauthorized"}
var Forbidden = Error{Code: 403, Message: "forbidden"}
var NotFound = Error{Code: 404, Message: "not found"}
var TooBusy = Error{Code: 503, Message: "overloaded"}
