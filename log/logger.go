package log

import (
	log "github.com/sirupsen/logrus"
	"io"
	golog "log"
	"os"
	"strings"
)

// a go logger whose write output pipes to logrus, to adapt for apis which
// only accept a go logger
var goLogger *golog.Logger = golog.New(new(WriteAdapter), "", 0)

// opens a log file for writing, if it is configured
func openWriter(outFile string) (io.Writer, error) {
	var w io.Writer = os.Stdout
	var err error = nil

	if "" != outFile {
		w, err = os.OpenFile(outFile, os.O_APPEND|os.O_CREATE|os.O_RDWR, 0666)
		if nil != err {
			return nil, err
		}
	}

	return w, nil
}

// initialize a logger. outFile == nil means log to stdout
func Init(levelStr string, outFile string) error {
	var writer io.Writer
	var err error
	if outFile != "" {
		writer, err = openWriter(outFile)
		if nil != err {
			return err
		}
	} else {
		writer = os.Stdout
	}

	log.SetOutput(writer)

	level, err := log.ParseLevel(levelStr)
	if nil != err {
		return err
	}
	log.SetLevel(level)

	return nil
}

// bridges the standard logger with logrus
type WriteAdapter struct{}

func (w *WriteAdapter) Write(p []byte) (n int, err error) {
	len := len(p)
	log.Info(strings.TrimSpace(string(p)))
	return len, nil
}

// returns the go logger adapter
func LogAdapter() *golog.Logger {
	return goLogger
}
