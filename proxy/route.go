package proxy

import (
	"github.com/wiloon/w-tcp-proxy/utils"
	"net"
	"sync"

	"github.com/wiloon/w-tcp-proxy/config"
	"github.com/wiloon/w-tcp-proxy/utils/logger"
)

type Route struct {
	source int
	target []int
	// key: backend conn id, value: address
	// key="0", value="192.168.50.100:2000"
	backends map[string]string
}

func (r *Route) InitBackendConn(split SplitFunc) {
	// dial
	for i, v := range r.backends {
		logger.Infof("create backend conn")
		// todo retry
		conn, err := net.Dial("tcp4", v)
		if err != nil {
			logger.Errorf("failed to dial backend server: %v", err)
			return
		}
		fd := utils.SocketFD(conn)
		scanner := NewScanner(make([]byte, 4096))
		scanner.split = split
		proxyConnections[fd] = &Connection{Conn: conn, Fd: fd, Id: i, Address: v, Scanner: scanner}
	}
}

type Rule struct {
	Key      string
	Backends []string
}

var RuleMap = sync.Map{}

func InitRoute() *Route {
	var backends = make(map[string]string)
	for _, v := range config.Instance.Backends {
		backends[v.Id] = v.Address
		bc := BackendConn{Id: v.Id}
		bc.Address = v.Address
	}
	var backendAddressList []string
	for _, v := range config.Instance.Route {

		for _, id := range v.BackendId {
			backendAddress := backends[id]
			logger.Debugf("route init, key: %s,backend address: %s", v.Key, backendAddress)
			backendAddressList = append(backendAddressList, backendAddress)
		}
		r := Rule{Key: v.Key, Backends: backendAddressList}
		RuleMap.Store(v.Key, r)
	}
	return nil
}
