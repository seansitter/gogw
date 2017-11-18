package auth

import (
	"errors"
	"fmt"
	"github.com/dgrijalva/jwt-go"
	"github.com/seansitter/gogw/httperr"
	log "github.com/sirupsen/logrus"
	"net/http"
	"strings"
)

// Returns a function which a stagehandler uses as an adapter to an authenticator.
// An AuthHandler takes an HTTP Request and returns an bool to indicate auth success
// and an optional http error (ie 401, 403)
func NewJWTAuthHandler(key interface{}) (AuthHandler, error) {
	authenticator, err := NewJWTAuthenticator(key) // the component that actually authenticates the token
	if nil != err {
		return nil, errors.New(fmt.Sprintf("failed to instantiate authenticator: %s", err))
	}

	return newJWTAuthHandler(authenticator, key)
}

// Returns a function which a stagehandler uses as an adapter to an authenticator with pooled workers for
// computationally expensive token validation
// An AuthHandler takes an HTTP Request and returns an bool to indicate auth success
// and an optional http error (ie 401, 403)
func NewPooledJWTAuthHandler(numWorkers int, key interface{}) (AuthHandler, error) {
	authenticator, err := NewPooledJWTAuthenticator(numWorkers, key) // the component that actually authenticates the token
	if nil != err {
		return nil, errors.New(fmt.Sprintf("failed to instantiate authenticator: %s", err))
	}

	return newJWTAuthHandler(authenticator, key)
}

func newJWTAuthHandler(authenticator Authenticator, key interface{}) (AuthHandler, error) {
	authHandlerFunc := func(r *http.Request) (bool, *httperr.Error) {
		authHeader := r.Header["Authorization"]
		if nil == authHeader {
			return false, &httperr.UnAuthorized
		}

		// TODO: this should find the bearer auth header if there are multiple
		bearerToken := authHeader[0]
		result, err := authenticator.Authenticate(bearerToken)

		if !result.Success || nil != err {
			if nil != err {
				log.Info(err)
			}
			// returning nil for error means authentication failed and will cause a 403 forbidden to client
			return false, nil
		}

		return true, nil
	}

	return authHandlerFunc, nil
}

type JWTAuthenticator struct {
	key interface{}
}

func NewJWTAuthenticator(key interface{}) (*JWTAuthenticator, error) {
	return &JWTAuthenticator{key}, nil
}

func (authenticator *JWTAuthenticator) Authenticate(creds interface{}) (*AuthResult, error) {
	token, err := splitToken(creds.(string))
	if nil != err {
		return &AuthResult{false, nil}, AuthError{err.Error()}
	}

	parsedToken, err := jwt.Parse(token, func(t *jwt.Token) (interface{}, error) {
		return authenticator.key, nil
	})

	if nil != err {
		return &AuthResult{false, nil}, AuthError{err.Error()}
	}
	if nil == parsedToken {
		return &AuthResult{false, nil}, AuthError{"authentication failed with a nil parsed token"}
	}

	return &AuthResult{true, parsedToken}, nil
}

type PooledJWTAuthenticator struct {
	workerPool    chan chan Job
	authenticator *JWTAuthenticator
}

// This struct is the return value from a authenticate call
type AuthReturn struct {
	AuthResult *AuthResult // true/false + optional artifact (ie decoded jwt token)
	AuthError  error
}

// Job represents the job to be run
type Job struct {
	Payload  interface{} // credentials
	RespChan chan AuthReturn
}

// Worker represents the worker that executes the job
type Worker struct {
	authenticator Authenticator
	workerRegPool chan chan Job
	inJobChannel  chan Job // the worker's own channel which is posted to workerRegPool when the worker is available
	quitChan      chan bool
}

func NewPooledJWTAuthenticator(numWorkers int, key interface{}) (*PooledJWTAuthenticator, error) {
	pool := make(chan chan Job, numWorkers)
	authenticator, err := NewJWTAuthenticator(key)
	if nil != err {
		return nil, err
	}

	// starting n number of workers
	for i := 0; i < numWorkers; i++ {
		worker := NewWorker(authenticator, pool)
		worker.Start()
	}

	return &PooledJWTAuthenticator{authenticator: authenticator, workerPool: pool}, nil
}

func NewWorker(authenticator Authenticator, workerRegPool chan chan Job) Worker {
	return Worker{
		authenticator: authenticator,
		workerRegPool: workerRegPool,   // the pool this worker will register on when its available
		inJobChannel:  make(chan Job),  // the worker's own channel where jobs are sent
		quitChan:      make(chan bool)} // channel where quit messages are senbt
}

// Start method starts the run loop for the worker, listening for a quit channel in
// case we need to stop it
func (w *Worker) Start() {
	go func() {
		for {
			// register the current worker into the worker queue.
			w.workerRegPool <- w.inJobChannel

			select {
			case job := <-w.inJobChannel:
				res, err := w.authenticator.Authenticate(job.Payload)
				job.RespChan <- AuthReturn{res, err}

			case <-w.quitChan:
				// we have received a signal to stop
				return
			}
		}
	}()
}

func (authenticator *PooledJWTAuthenticator) Authenticate(token interface{}) (*AuthResult, error) {
	inJobChan := <-authenticator.workerPool // get access to a works job channel
	authRetChan := make(chan AuthReturn)
	inJobChan <- Job{token.(string), authRetChan}
	authRet := <-authRetChan
	close(authRetChan)
	return authRet.AuthResult, authRet.AuthError
}

func splitToken(token string) (string, error) {
	tokenParts := strings.SplitN(token, " ", 2)
	if len(tokenParts) == 2 {
		if strings.ToLower(tokenParts[0]) == "bearer" {
			token = tokenParts[1]
		} else {
			return "", errors.New("not a bearer token")
		}
	}

	return token, nil
}
