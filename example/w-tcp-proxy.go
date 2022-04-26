package main

import (
	"flag"
	"github.com/wiloon/w-tcp-proxy/proxy"
	"github.com/wiloon/w-tcp-proxy/utils"
	"github.com/wiloon/w-tcp-proxy/utils/config"
	"github.com/wiloon/w-tcp-proxy/utils/logger"
)

var (
	listenPort = flag.String("listen", "2000", "listening port")
	// targetAddress = flag.String("backend", "127.0.0.1:3000", "backend address")
	// targetAddress = flag.String("backend", "192.168.122.1:22", "backend address")
	// targetAddress = flag.String("backend", "10.71.20.6:22", "backend address")
	// targetAddress = flag.String("backend", "192.168.50.100:22", "backend address")
	backendMain           = flag.String("backend-main", "10.71.20.6:4100", "backend address, separate by ','")
	backendReplicator     = flag.String("backend-replicator", "10.71.20.181:6100", "backend address, separate by ','")
	backendReplicatorMode = flag.String("backend-replicator-mode", "copy", "replicator mode: copy, forward")
)

func main() {
	flag.Parse()
	config.Init()
	cfg := config.Instance
	logger.InitTo(
		cfg.Log.Console,
		cfg.Log.File,
		cfg.Log.FileLevel,
		cfg.Project.Name,
	)

	p := proxy.NewProxy(*listenPort, *backendMain, *backendReplicator, *backendReplicatorMode)
	p.Split(split0)
	p.TokenHandler(tkHandler)
	p.Start()

	utils.WaitSignals(func() {
		logger.Infof("proxy shutting down")
	})
}

func split0(data []byte, eof bool) (advance int, token []byte, err error) {
	token = data
	return len(data), token, err
}

func tkHandler(token []byte) {
	logger.Debugf("token: %s", string(token))
}
