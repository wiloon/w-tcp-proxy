package proxy

import (
	"github.com/wiloon/w-tcp-proxy/config"
	"github.com/wiloon/w-tcp-proxy/utils/logger"
	"sync"
)

type Route struct {
	// key: backend conn id, value: address
	// key = id, value = Connection
	backends map[string]*Connection
	Rules    sync.Map
}

func (r *Route) UpdateRule(c Connection) {
	r.Rules.Range(func(key, value any) bool {
		rule := value.(Rule)
		if rule.Type == routeTypeForward {
			rule.Backends[0] = c
		}
		return true
	})
}

type Rule struct {
	Key      string
	Type     string
	Backends []Connection
}

const routeTypeCopy = "copy"
const routeTypeForward = "forward"

func InitRoute() *Route {
	r := Route{}
	r.backends = make(map[string]*Connection)

	for _, v := range config.Instance.Backends {
		r.backends[v.Id] = &Connection{RouteId: v.Id, Address: v.Address, Backend: true, Default: v.Default}
		logger.Debug("route backend, id: %s, address: %s", v.Id, v.Address)
	}

	for _, v := range config.Instance.Route {
		rule := Rule{Key: v.Key}
		rule.Type = v.Type
		if v.Type == routeTypeCopy {
			for _, backendConnConfig := range r.backends {
				rule.Backends = append(rule.Backends, *backendConnConfig)
			}
		} else if v.Type == routeTypeForward {
			rule.Backends = append(rule.Backends, *r.backends[v.BackendId])
		}
		r.Rules.Store(v.Key, rule)
		logger.Debugf("rule, key: %s, type: %s", v.Key, v.Type)
	}
	return &r
}
