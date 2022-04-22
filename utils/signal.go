package utils

import (
	"fmt"
	"github.com/wiloon/w-tcp-proxy/utils/logger"
	"os"
	"os/signal"
	"syscall"
)

var signals = make(chan os.Signal)

func init() {
	signal.Notify(signals, os.Interrupt, os.Kill, syscall.SIGTERM)
}

type AppExitHandler func()

func WaitSignals(appExitHandler AppExitHandler) {
	for s := range signals {
		if s == os.Interrupt || s == os.Kill || s == syscall.SIGTERM {
			fmt.Printf("\n")
			appExitHandler()
			logger.Infof("---\n")
			logger.Sync()
			break
		}
	}
	signal.Stop(signals)
}
