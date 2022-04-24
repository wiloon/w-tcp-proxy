package main

import (
	"flag"
	"github.com/wiloon/w-tcp-proxy/proxy"
	"github.com/wiloon/w-tcp-proxy/utils"
	"github.com/wiloon/w-tcp-proxy/utils/logger"
)

var (
	listenPort = flag.String("listen", "2000", "listening port")
	// targetAddress = flag.String("backend", "127.0.0.1:3000", "backend address")
	// targetAddress = flag.String("backend", "192.168.122.1:22", "backend address")
	// targetAddress = flag.String("backend", "10.61.20.6:22", "backend address")
	targetAddress = flag.String("backend", "10.61.20.6:22", "backend address")
)

func main() {
	flag.Parse()
	p := proxy.NewProxy(*listenPort, *targetAddress)
	p.Split(split0)
	p.Start()

	utils.WaitSignals(func() {
		logger.Infof("proxy shutting down")
	})
}

func split0(data []byte, eof bool) (advance int, token []byte, err error) {
	token = data
	return len(data), token, err
}
