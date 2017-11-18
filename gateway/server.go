package gateway

import (
	"context"
	"errors"
	"github.com/dgrijalva/jwt-go"
	"github.com/seansitter/gogw/auth"
	"github.com/seansitter/gogw/config"
	"github.com/seansitter/gogw/res"
	log "github.com/sirupsen/logrus"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"
)

const numAuthWorkers = 8

// Gateway definition
type GwServer struct {
	httpServer http.Server
}

// Reads and decodes a pem file from the asset path
func newRSAPublicKey(PEMAssetPath string) (interface{}, error) {
	if PEMAssetPath == "" {
		return nil, errors.New("no pemfile set on config gateway")
	}

	// load the pem from the asset path
	pemCtnt := res.MustAsset(PEMAssetPath)
	pk, err := jwt.ParseRSAPublicKeyFromPEM(pemCtnt)
	if nil != err {
		return nil, err
	}

	return pk, nil
}

// The dispatcher is the primary handler or the server
func newDispatcher(config config.Config) (*Dispatcher, error) {
	key, err := newRSAPublicKey(config.Gateway.PEMFile)
	if nil != err {
		return nil, err
	}

	log.Infof("using %v auth workers", config.Gateway.AuthWorkers)
	authHandler, err := auth.NewPooledJWTAuthHandler(config.Gateway.AuthWorkers, key)
	//authHandler, err := auth.NewJWTAuthHandler(key)
	if nil != err {
		return nil, err
	}

	dispatcher := NewDispatchBuilder().
		ProxyConfig(config.Proxy).
		CircuitBreakerConfig(config.CircuitBreaker).
		Endpoints(config.Endpoints).
		AuthHandler(authHandler).
		Build()

	return dispatcher, nil
}

func NewServer(config config.Config) (*GwServer, error) {
	dispatcher, err := newDispatcher(config)
	if nil != err {
		return nil, err
	}

	s := http.Server{
		Addr:           ":" + strconv.Itoa(config.Server.Port),
		Handler:        dispatcher,
		ReadTimeout:    config.Server.ReadTimeoutMs * time.Millisecond,
		WriteTimeout:   config.Server.WriteTimeoutMs * time.Millisecond,
		MaxHeaderBytes: 1 << 20,
	}

	return &GwServer{s}, nil
}

func (s *GwServer) Run() error {
	stopChan := make(chan os.Signal)
	errChan := make(chan error)

	signal.Notify(stopChan, syscall.SIGINT, syscall.SIGTERM)

	// this calll blocks, so execute in a goroutine so we can handle shutdown
	go func() {
		if err := s.httpServer.ListenAndServe(); err != nil {
			errChan <- err
		}
	}()

	select {
	case err := <-errChan:
		return err
	case <-stopChan:
		return s.Shutdown() // got the shutdown signal
	}

}

func (s *GwServer) Shutdown() error {
	log.Info("shutting down the server...")
	ctx, _ := context.WithTimeout(context.Background(), 5*time.Second)
	err := s.httpServer.Shutdown(ctx)
	log.Info("server gracefully stopped")
	return err
}
