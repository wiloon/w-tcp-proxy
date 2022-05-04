package proxy

// key: fd, value: proxyConnection
var proxyConnections = make(map[int]*proxyConnection)
