package config

import (
	"github.com/pelletier/go-toml/v2"
	"log"
)

var Instance WTcpProxyConfig

type Project struct {
	Name string
}

//goland:noinspection GoUnusedConst
const LogToFile = "file"
const LogLevelDebug = "debug"

//goland:noinspection GoUnusedConst
const LogLevelInfo = "info"

type Log struct {
	Console      bool
	ConsoleLevel string
	File         bool
	FileLevel    string
}
type Backend struct {
	Id      string
	Address string
	Default bool
}
type Route struct {
	Key       string
	BackendId []string
}
type WTcpProxyConfig struct {
	Project  Project
	Log      Log
	Backends []Backend
	Route    []Route
}

var configFilePath string

func Init() {
	b, err := LoadDefaultConfigFile()
	if err != nil {
		log.Printf("failed to load config: %v", err)
	}

	err = toml.Unmarshal(b, &Instance)
	if err != nil {
		panic(err)
	}
}
