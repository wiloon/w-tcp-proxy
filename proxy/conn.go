package proxy

import "net"

// Connection inbound connection, backend connection
type Connection struct {
	Id      string
	Address string
	Fd      int
	Conn    net.Conn
	Scanner *Scanner
}

// key: fd, value: proxyConnection
var proxyConnections = make(map[int]*Connection)
