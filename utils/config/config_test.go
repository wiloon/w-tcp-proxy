package config

import (
	"fmt"
	"github.com/pelletier/go-toml/v2"
	"testing"
)

func Test0(t *testing.T) {
	cfg := WTcpProxyConfig{
		Project: Project{Name: "w-tcp-proxy"},
		Log:     Log{Console: true, ConsoleLevel: LogLevelDebug, File: true, FileLevel: LogLevelDebug},
		Backends: []Backend{
			{Id: "0", Address: "192.168.0.1", Default: true},
			{Id: "1", Address: "192.168.0.2"},
		},
		Route: []Route{
			{Key: "key0", BackendId: []string{"0", "1"}},
			{Key: "key1", BackendId: []string{"0"}},
		},
	}

	b, err := toml.Marshal(cfg)
	if err != nil {
		panic(err)
	}
	fmt.Println(string(b))
}
