package route

import (
	"github.com/wiloon/w-tcp-proxy/config"
	"sync"
)

type Rule struct {
	Key      string
	Backends []string
}

var RuleMap = sync.Map{}

func Init() {
	var backends map[string]string
	for _, v := range config.Instance.Backends {
		backends[v.Id] = v.Address
	}
	var backendAddressList []string
	for _, v := range config.Instance.Route {

		for _, id := range v.BackendId {
			backendAddress := backends[id]
			backendAddressList = append(backendAddressList, backendAddress)
		}
		r := Rule{Key: v.Key, Backends: backendAddressList}
		RuleMap.Store(v.Key, r)
	}
}
