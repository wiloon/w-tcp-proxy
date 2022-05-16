package proxy

import (
	"net"
	"sync"
)

// Connection inbound connection, backend connection
type Connection struct {
	// connection id from config file
	RouteId string
	// ip:port
	Address string
	// fd
	Fd int
	// go conn
	Conn net.Conn
	// buf scanner
	Scanner *Scanner
	// is backend conn
	Backend bool
	// is default backend conn
	Default bool
}

func (c *Connection) Dial() {
	
}
// key: fd, value: proxyConnection
var proxyConnections = sync.Map{}
