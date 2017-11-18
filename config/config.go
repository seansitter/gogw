package config

import (
	"errors"
	"fmt"
	"gopkg.in/yaml.v2"
	"time"
)

const defaultLoglevel = "info"

type Endpoint struct {
	Name            string
	Key             string
	URL             string
	Authenticate    bool
	SharedTransport string `yaml:"sharedTransport"`
}

type Proxy struct {
	MaxIdleConns            int           `yaml:"maxIdleConns"`
	MaxIdleConnsPerHost     int           `yaml:"maxIdleConnsPerHost"`
	IdleConnTimeoutMs       time.Duration `yaml:"idleConnTimeoutMs"`
	TLSHandshakeTimeoutMs   time.Duration `yaml:"tlsHandshakeTimeoutMs"`
	ExpectContinueTimeoutMs time.Duration `yaml:"expectContinueTimeoutMs"`
	DialTimeoutMs           time.Duration `yaml:"dialTimeoutMs"`
	DialKeepAliveMs         time.Duration `yaml:"dialKeepAliveMs"`
	ResponseHeaderTimeoutMs time.Duration `yaml:"responseHeaderTimeoutMs"` // ie: time to first byte
}

type CircuitBreaker struct {
	MaxHalfOpenRequests         uint32        `yaml:"maxHalfOpenRequests"`
	ClearFailureCountIntervalMs time.Duration `yaml:"clearFailureCountIntervalMs"`
	HalfOpenAfterMs             time.Duration `yaml:"halfOpenAfterMs"`
	FailuresToOpen              int           `yaml:"failuresToOpen"`
}

type Server struct {
	Port           int
	ReadTimeoutMs  time.Duration `yaml:"readTimeoutMs"`
	WriteTimeoutMs time.Duration `yaml:"writeTimeoutMs"`
}

type Gateway struct {
	PEMFile     string `yaml:"pemfile"`
	AuthWorkers int    `yaml:"authWorkers"`
}

type Logger struct {
	Level string
	File  string
}

type Config struct {
	Gateway        Gateway
	Endpoints      []Endpoint
	Server         Server
	Proxy          Proxy
	CircuitBreaker *CircuitBreaker `yaml:"circuitBreaker"`
	Logger         *Logger
}

// copy an endpoint
func (ep *Endpoint) Copy() *Endpoint {
	return &Endpoint{ep.Name, ep.Key, ep.URL, ep.Authenticate, ep.SharedTransport}
}

// to string method for an endpoint
func (ep Endpoint) String() string {
	return fmt.Sprintf("endpoint [name: %v, key: %v, url: %v]", ep.Name, ep.Key, ep.URL)
}

// validates a single endpoint
func (ep *Endpoint) valid() (bool, error) {
	if ep.Name == "" {
		return false, errors.New("missing name for endpoint")
	}
	if ep.Key == "" {
		return false, errors.New("missing key for endpoint: " + ep.Name)
	}
	if ep.URL == "" {
		return false, errors.New("missing url for endpoint: " + ep.Name)
	}

	return true, nil
}

// validates endpoint configuration
func (config *Config) validateEndpoints() (bool, error) {
	epNameSet := make(map[string]bool)
	epKeySet := make(map[string]bool)

	for _, ep := range config.Endpoints {
		if _, ok := epNameSet[ep.Name]; ok {
			return false, errors.New(fmt.Sprintf("endpoint name: '%v' is not unique", ep.Name))
		}
		epNameSet[ep.Name] = true

		if _, ok := epKeySet[ep.Key]; ok {
			return false, errors.New(fmt.Sprintf("endpoint key: '%v' is not unique", ep.Key))
		}
		epKeySet[ep.Key] = true

		if ep.SharedTransport != "" && ep.SharedTransport == ep.Name {
			return false, errors.New(fmt.Sprintf("endpoint name: '%v' cannot share a transport with itself", ep.Name))
		}

		if v, err := ep.valid(); !v {
			return v, err
		}
	}

	return true, nil
}

// ensure sensible defaults for the server
func (config *Config) setServerDefaults() {
	if config.Server.Port == 0 {
		config.Server.Port = 8080
	}

	if config.Server.ReadTimeoutMs == 0 {
		config.Server.ReadTimeoutMs = 10000
	}

	if config.Server.WriteTimeoutMs == 0 {
		config.Server.WriteTimeoutMs = 10000
	}
}

// ensure sensible defaults for the proxy
func (config *Config) setProxyDefaults() {
	if config.Proxy.DialKeepAliveMs == 0 {
		config.Proxy.DialKeepAliveMs = 10000
	}
	if config.Proxy.DialKeepAliveMs == 0 {
		config.Proxy.DialKeepAliveMs = 10000
	}
	if config.Proxy.MaxIdleConns == 0 {
		config.Proxy.MaxIdleConns = 100
	}
	if config.Proxy.MaxIdleConnsPerHost == 0 {
		config.Proxy.MaxIdleConnsPerHost = 10
	}
	if config.Proxy.IdleConnTimeoutMs == 0 {
		config.Proxy.IdleConnTimeoutMs = 30000
	}
	if config.Proxy.ResponseHeaderTimeoutMs == 0 {
		config.Proxy.ResponseHeaderTimeoutMs = 3000
	}
	if config.Proxy.TLSHandshakeTimeoutMs == 0 {
		config.Proxy.TLSHandshakeTimeoutMs = 500
	}
	if config.Proxy.ExpectContinueTimeoutMs == 0 {
		config.Proxy.ExpectContinueTimeoutMs = 500
	}
}

// ensure sensible defaults for the circuit breaker
func (config *Config) setCircuitBreakerDefaults() {
	if config.CircuitBreaker.MaxHalfOpenRequests == 0 {
		config.CircuitBreaker.MaxHalfOpenRequests = 1
	}
	if config.CircuitBreaker.ClearFailureCountIntervalMs == 0 {
		config.CircuitBreaker.ClearFailureCountIntervalMs = 10000
	}
	if config.CircuitBreaker.HalfOpenAfterMs == 0 {
		config.CircuitBreaker.HalfOpenAfterMs = 5000
	}
	if config.CircuitBreaker.FailuresToOpen == 0 {
		config.CircuitBreaker.FailuresToOpen = 10
	}
}

// ensure sensible defaults for the gateway
func (config *Config) setGatewayDefaults() {
	if config.Gateway.AuthWorkers == 0 {
		config.Gateway.AuthWorkers = 4
	}
}

func (config *Config) setLoggerDefaults() {
	if nil == config.Logger {
		config.Logger = &Logger{defaultLoglevel, ""}
	} else if config.Logger.Level == "" {
		config.Logger.Level = defaultLoglevel
	}
}

// parses the config file
func Parse(ctnt []byte) (*Config, error) {
	c := new(Config)

	err := yaml.Unmarshal(ctnt, &c)
	if nil != err {
		return nil, errors.New(fmt.Sprintf("failed to parse yaml: %v", err))
	}

	if v, err := c.validateEndpoints(); !v {
		return nil, err
	}

	c.setServerDefaults()
	c.setProxyDefaults()
	c.setCircuitBreakerDefaults()
	c.setGatewayDefaults()
	c.setLoggerDefaults()

	return c, nil
}
