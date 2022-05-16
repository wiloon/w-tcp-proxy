package main

import (
	"github.com/wiloon/w-tcp-proxy/config"
	"github.com/wiloon/w-tcp-proxy/proxy"
	"github.com/wiloon/w-tcp-proxy/utils"
	"github.com/wiloon/w-tcp-proxy/utils/logger"
)

func main() {
	config.Init()
	cfg := config.Instance
	logger.InitTo(
		cfg.Log.Console,
		cfg.Log.File,
		cfg.Log.FileLevel,
		cfg.Project.Name,
	)

	logger.Debugf("project name: %s", cfg.Project.Name)

	p := proxy.NewProxy(cfg.Project.Port)
	p.Split(split0)

	route := proxy.InitRoute()
	p.BindRoute(route)
	p.TokenHandler(tkHandler)
	p.Start()

	utils.WaitSignals(func() {
		logger.Infof("proxy shutting down")
	})
}

func split0(data []byte, eof bool) (advance int, key, token []byte, err error) {
	token = data
	key = []byte("key0")
	return len(data), key, token, err
}

func tkHandler(token []byte) {
	logger.Debugf("token: %s", string(token))
}
