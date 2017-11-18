package gateway

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/seansitter/gogw/auth"
	"github.com/seansitter/gogw/config"
	"github.com/seansitter/gogw/httperr"
	gwlog "github.com/seansitter/gogw/log"
	log "github.com/sirupsen/logrus"
	"github.com/sony/gobreaker"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// builder for the dispatcher, allows for proper construction
type DispatcherBuilder struct {
	endpoints  []config.Endpoint
	dispatcher *Dispatcher
}

func NewDispatchBuilder() *DispatcherBuilder {
	db := new(DispatcherBuilder)
	db.dispatcher = new(Dispatcher)
	return db
}

func (b *DispatcherBuilder) ProxyConfig(c config.Proxy) *DispatcherBuilder {
	b.dispatcher.proxyConfig = c
	return b
}

func (b *DispatcherBuilder) CircuitBreakerConfig(c *config.CircuitBreaker) *DispatcherBuilder {
	b.dispatcher.cbConfig = c
	return b
}

func (b *DispatcherBuilder) AuthHandler(h auth.AuthHandler) *DispatcherBuilder {
	b.dispatcher.authHandler = h
	return b
}

func (b *DispatcherBuilder) Endpoints(e []config.Endpoint) *DispatcherBuilder {
	b.endpoints = e
	return b
}

func (b *DispatcherBuilder) Build() *Dispatcher {
	b.dispatcher.transports = make(map[string]http.RoundTripper)
	b.dispatcher.configureRoutes(b.endpoints)
	return b.dispatcher
}

// executes a single stage in the request pipeline
type ExecHandler func(http.ResponseWriter, *http.Request) bool

type StageHandler struct {
	ExecHandler ExecHandler
	Next        *StageHandler
}

type HandlerExecutor interface {
	Execute(w http.ResponseWriter, r *http.Request) bool
}

// StageHandler implements the HandlerExecutor interface
func (h *StageHandler) Execute(w http.ResponseWriter, r *http.Request) {
	if h.ExecHandler(w, r) && nil != h.Next {
		h.Next.Execute(w, r)
	}
}

// A route configures an endpoint with the handler for the endpoint.
// Chiefly, the endpoint may have an authentication handler prior to the proxy handler
type Route struct {
	Endpoint     *config.Endpoint
	StageHandler *StageHandler
}

type Dispatcher struct {
	routes      map[string]Route
	authHandler auth.AuthHandler
	proxyConfig config.Proxy
	cbConfig    *config.CircuitBreaker
	transports  map[string]http.RoundTripper
}

// A circuitbreaker is tied to a transport, which encapsulates the client/connection managment to and endpoint
func newCircuitBreaker(name string, cbConfig *config.CircuitBreaker) *gobreaker.CircuitBreaker {
	if nil == cbConfig {
		return nil
	}

	cbSettings := gobreaker.Settings{
		Interval:    cbConfig.ClearFailureCountIntervalMs * time.Millisecond,
		MaxRequests: cbConfig.MaxHalfOpenRequests,
		Name:        fmt.Sprintf("crctbrkr-%v", name),
		Timeout:     cbConfig.HalfOpenAfterMs * time.Millisecond,
		OnStateChange: func(name string, from gobreaker.State, to gobreaker.State) {
			log.Errorf("circuit breaker %v transitioned from: %v to: %v", name, from, to)
		},
	}

	return gobreaker.NewCircuitBreaker(cbSettings)
}

// A transport is a connection-managing client
func newTransport(proxyConfig config.Proxy) http.RoundTripper {
	return &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   proxyConfig.DialTimeoutMs * time.Millisecond,
			KeepAlive: proxyConfig.DialKeepAliveMs * time.Millisecond,
			DualStack: true,
		}).DialContext,
		MaxIdleConns:          proxyConfig.MaxIdleConns,
		IdleConnTimeout:       proxyConfig.IdleConnTimeoutMs * time.Millisecond,
		TLSHandshakeTimeout:   proxyConfig.TLSHandshakeTimeoutMs * time.Millisecond,
		ExpectContinueTimeout: proxyConfig.ExpectContinueTimeoutMs * time.Millisecond,
	}
}

// This struct ties together a circuit breaker and transport. It implements the RoundTrip interface,
// which is the core interface of a transport. This allows it to be used in the go native
// reverse proxy without any other modifications.
type CbTransport struct {
	Transport      http.RoundTripper
	CircuitBreaker *gobreaker.CircuitBreaker
}

// Instantiates a CbTransport
func newCbTransport(name string, proxyConfig config.Proxy, cbConfig *config.CircuitBreaker) http.RoundTripper {
	return &CbTransport{
		Transport:      newTransport(proxyConfig),
		CircuitBreaker: newCircuitBreaker(name, cbConfig),
	}
}

// RoundTrip interface is implemented by CbTransport so that we can have a transport with a circuitbreaker
func (transport *CbTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	var resp interface{}
	var err error

	if nil == transport.CircuitBreaker {
		resp, err = transport.Transport.RoundTrip(request)
	} else {
		resp, err = transport.CircuitBreaker.Execute(func() (interface{}, error) {
			r, e := transport.Transport.RoundTrip(request)
			if nil != r && r.StatusCode >= 500 && r.StatusCode < 600 {
				return nil, errors.New("500 error for service endpoint")
			}
			return r, e
		})
	}

	if nil == resp {
		return nil, err
	}

	return resp.(*http.Response), err
}

// Creates a StageHandler which proxies the request to an endpoint
func (d *Dispatcher) newProxyStageHandler(ep config.Endpoint) (*StageHandler, error) {
	proxyUrl, err := url.Parse(ep.URL)
	if nil != err {
		return nil, errors.New(fmt.Sprintf("failed to parse url: %v, for endpoint: %v, %v", ep.URL, ep.Name, err))
	}

	transName := ep.Name

	if ep.SharedTransport != "" {
		transName = ep.SharedTransport
	}

	if v, ok := d.transports[transName]; ok {
		d.transports[transName] = v
	} else {
		d.transports[transName] = newCbTransport(ep.Name, d.proxyConfig, d.cbConfig)
	}

	// create the reverse proxy
	routeProxy := httputil.NewSingleHostReverseProxy(proxyUrl)
	routeProxy.Transport = d.transports[transName]
	routeProxy.ErrorLog = gwlog.LogAdapter()

	sh := &StageHandler{
		Next: nil,
		ExecHandler: func(w http.ResponseWriter, r *http.Request) bool {
			routeProxy.ServeHTTP(w, r)
			return false
		},
	}

	return sh, nil
}

// Creates a StageHandler chain which authenticates before proxying
func (d *Dispatcher) newAuthenticatingProxyStageHandler(ep config.Endpoint) (*StageHandler, error) {
	if nil == d.authHandler {
		return nil, errors.New("authenticated service configured but no auth handler set")
	}

	proxySh, err := d.newProxyStageHandler(ep)
	if nil != err {
		return nil, err
	}

	sh := &StageHandler{
		Next: proxySh,
		ExecHandler: func(w http.ResponseWriter, r *http.Request) bool {
			if ok, httpErr := d.authHandler(r); !ok {
				if nil != httpErr {
					if httpErr.Code != 0 {
						w.WriteHeader(httpErr.Code)
					}
					if httpErr.Message != "" {
						w.Write([]byte(httpErr.Message))
					}
				} else {
					w.WriteHeader(403)
					w.Write([]byte("forbidden"))
				}

				return false
			}
			return true
		},
	}

	return sh, nil
}

func (d *Dispatcher) configureRoutes(endpoints []config.Endpoint) (*Dispatcher, error) {
	// build routes
	routes := make(map[string]Route)

	for _, ep := range endpoints {
		var sh *StageHandler
		var err error
		if ep.Authenticate {
			sh, err = d.newAuthenticatingProxyStageHandler(ep)
		} else {
			sh, err = d.newProxyStageHandler(ep)
		}

		if nil != err {
			return nil, err
		}

		routes[ep.Name] = Route{Endpoint: ep.Copy(), StageHandler: sh}
	}

	d.routes = routes

	return d, nil
}

// Sends an error response, used when an immediate error response is called for (ie 404)
func (dispatcher *Dispatcher) sendError(w http.ResponseWriter, e httperr.Error) {
	w.WriteHeader(e.Code)
	fmt.Fprintf(w, "%v: %v", e.Message, e.Code)
}

// Dispatcher implements the ServeHTTP interface so that it can be used directly as a
// handler for the golang http server
func (dispatcher *Dispatcher) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	dispatcher.Dispatch(w, r) // defer to dispatch method
}

func (dispatcher *Dispatcher) Dispatch(w http.ResponseWriter, r *http.Request) error {
	pathSgmts := strings.Split(r.URL.Path, "/")
	if len(pathSgmts) >= 1 {
		if matchRoute, ok := dispatcher.routes[pathSgmts[1]]; ok {
			sh := matchRoute.StageHandler
			for nil != sh {
				if !sh.ExecHandler(w, r) {
					break
				}
				sh = matchRoute.StageHandler.Next
			}
		} else {
			dispatcher.sendError(w, httperr.NotFound)
		}
	} else {
		dispatcher.sendError(w, httperr.NotFound)
	}

	return nil
}

/**
Gets the go function id
**/
func getGID() uint64 {
	b := make([]byte, 64)
	b = b[:runtime.Stack(b, false)]
	b = bytes.TrimPrefix(b, []byte("goroutine "))
	b = b[:bytes.IndexByte(b, ' ')]
	n, _ := strconv.ParseUint(string(b), 10, 64)
	return n
}
