package proxy

import "net"

// Connection inbound connection, backend connection
type Connection struct {
	Address string
	Fd      int
	Conn    net.Conn
	Scanner *Scanner
}

type backendConn struct {
	Connection
	Id string
}

// key: fd, value: proxyConnection
var proxyConnections = make(map[int]*Connection)
