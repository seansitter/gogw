package main

import (
	"flag"
	"fmt"
	"github.com/seansitter/gogw/config"
	"github.com/seansitter/gogw/gateway"
	loginit "github.com/seansitter/gogw/log"
	"github.com/seansitter/gogw/res"
	log "github.com/sirupsen/logrus"
	"os"
)

const DefaultEnv = "local"

var env string

func main() {
	loginit.Init("debug", "") // initial logger is trace to stdout, until we read the config

	initOptions()
	var c *config.Config = initConfig()

	initLogger(c.Logger)
	runServer(*c)
	log.Info("exiting...")
	os.Exit(0)
}

func initLogger(loggerConfig *config.Logger) {
	err := loginit.Init(loggerConfig.Level, loggerConfig.File)
	if nil != err {
		log.Error(err)
		os.Exit(1)
	}
}

func initOptions() {
	// get the env from environment variable or commandline arg
	pEnv := flag.String("env", "", "the environment")
	env = *pEnv

	if "" == env && "" != os.Getenv("GWENV") {
		env = os.Getenv("GWENV")
	}
	if "" == env {
		env = DefaultEnv
	}

	log.Infof("running in env: %v", env)
}

func initConfig() *config.Config {
	ctnt := res.MustAsset(fmt.Sprintf("assets/config-%s.yml", env))
	c, err := config.Parse(ctnt)
	if nil != err {
		log.Errorf("failed to load config: %v", err)
		os.Exit(1)
	}

	return c
}

func runServer(c config.Config) {
	if len(c.Endpoints) == 0 {
		log.Error("failed to find endpoints in config")
		os.Exit(1)
	}

	for _, e := range c.Endpoints {
		log.Infof("found endpoint: %s", e)
	}

	log.Infof("starting server on port: %v", c.Server.Port)
	gw, err := gateway.NewServer(c)
	if nil != err {
		log.Error(err)
		os.Exit(1)
	}

	err = gw.Run()

	if nil != err {
		log.Error(err)
		os.Exit(1)
	}
}
