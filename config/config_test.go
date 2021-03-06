package config

import (
	"fmt"
	"github.com/pelletier/go-toml/v2"
	"testing"
)

func Test0(t *testing.T) {
	cfg := WTcpProxyConfig{
		Project: Project{Name: "w-tcp-proxy", Port: 2000},
		Log:     Log{Console: true, ConsoleLevel: LogLevelDebug, File: true, FileLevel: LogLevelDebug},
		Backends: []Backend{
			{Id: "0", Address: "192.168.0.1", Default: true},
			{Id: "1", Address: "192.168.0.2"},
		},
		Route: []Route{
			{Key: "key0", Type: "copy"},
			{Key: "key1", Type: "forward", BackendId: "0"},
		},
	}

	b, err := toml.Marshal(cfg)
	if err != nil {
		panic(err)
	}
	fmt.Println(string(b))
}
