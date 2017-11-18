package auth

import (
	"github.com/seansitter/gogw/httperr"
	"net/http"
)

// An AuthHandler is an adapter function which takes a request and returns a bool for
// successful authentication and an optional http error
type AuthHandler func(r *http.Request) (bool, *httperr.Error)

type AuthError struct {
	msg string
}

func (err AuthError) Error() string {
	return err.msg
}

type AuthResult struct {
	Success  bool
	Artifact interface{} // optional artiface of authentication (ie, jwt token)
}

type Authenticator interface {
	Authenticate(creds interface{}) (*AuthResult, error)
}
