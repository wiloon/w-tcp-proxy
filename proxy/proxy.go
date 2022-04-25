package proxy

import (
	"encoding/hex"
	"fmt"
	"github.com/wiloon/w-tcp-proxy/utils"
	"github.com/wiloon/w-tcp-proxy/utils/logger"
	"net"
	"sync"
)

type TokenHandlerFunc func(token []byte)

type Proxy struct {
	ListenPort     string
	BackendAddress string
	Epoll          *utils.Epoll
	Connections    *sync.Map
	split          SplitFunc
	tokenHandler   TokenHandlerFunc
}

type proxyConn struct {
	InboundFd       int
	InboundConn     net.Conn
	InboundScanner  *Scanner
	OutboundFd      int
	OutboundConn    net.Conn
	OutboundScanner *Scanner
}

func (pc *proxyConn) GetTargetConn(fd int) net.Conn {
	if fd == pc.InboundFd {
		return pc.OutboundConn
	} else if fd == pc.OutboundFd {
		return pc.InboundConn
	}
	return nil
}

func (pc *proxyConn) Close() {
	err := pc.InboundConn.Close()
	if err != nil {
		logger.Errorf("failed to close conn: %v", pc.InboundConn.RemoteAddr().String())
	}
	err = pc.OutboundConn.Close()
	if err != nil {
		logger.Errorf("failed to close conn: %v", pc.InboundConn.RemoteAddr().String())
	}
}

func (pc *proxyConn) appendBuf(fd int, data []byte) {
	if fd == pc.InboundFd {
		pc.InboundScanner.appendBuf(data)
	} else {
		pc.OutboundScanner.appendBuf(data)
	}
}

func (pc *proxyConn) Scan(fd int) bool {
	if fd == pc.InboundFd {
		return pc.InboundScanner.Scan()
	} else {
		return pc.OutboundScanner.Scan()
	}
}

func (pc *proxyConn) Bytes(fd int) []byte {
	if fd == pc.InboundFd {
		return pc.InboundScanner.Bytes()
	} else {
		return pc.OutboundScanner.Bytes()
	}
}

type connData struct {
	Fd   int
	Data []byte
}

func (c *connData) String() string {
	return fmt.Sprintf("fd: %d, data: %s", c.Fd, hex.EncodeToString(c.Data))
}

func (p *Proxy) Start() {
	logger.InitTo(true, true, "debug", "w-tcp-proxy")

	utils.SetNoFileLimit(10000, 20000)
	inboundListener, err := net.Listen("tcp", ":"+p.ListenPort)
	if err != nil {
		logger.Errorf("failed to listen: %s, err: %v", p.ListenPort, err)
		return
	}
	logger.Infof("proxy listening on: %s", p.ListenPort)

	ch0 := make(chan *connData, 100)

	go p.proxy(p.Epoll, ch0)

	go p.consume(ch0, p.Connections)

	go func() {
		for {
			sourceConn, err := inboundListener.Accept()
			if err != nil {
				return
			}
			sourceConnFd := utils.SocketFD(sourceConn)
			logger.Infof("inbound conn: %s, fd: %d", sourceConn.RemoteAddr().String(), sourceConnFd)

			// dial target
			targetConn, err := net.Dial("tcp4", p.BackendAddress)
			if err != nil {
				logger.Errorf("failed to dial backend server %v", err)
				return
			}
			targetConnFd := utils.SocketFD(targetConn)
			logger.Infof("proxy, inbound: %s>%s, fd: %d, outbound: %s>%s, fd: %d",
				sourceConn.RemoteAddr().String(), sourceConn.LocalAddr().String(), sourceConnFd, targetConn.LocalAddr().String(), targetConn.RemoteAddr().String(), targetConnFd)

			pc := &proxyConn{
				InboundFd:       sourceConnFd,
				InboundConn:     sourceConn,
				InboundScanner:  NewScanner(make([]byte, 4096)),
				OutboundFd:      targetConnFd,
				OutboundConn:    targetConn,
				OutboundScanner: NewScanner(make([]byte, 4096)),
			}
			pc.InboundScanner.split = p.split
			pc.OutboundScanner.split = p.split
			p.Connections.Store(sourceConnFd, pc)
			p.Connections.Store(targetConnFd, pc)

			if err := p.Epoll.Add(sourceConn); err != nil {
				logger.Errorf("failed to add connection %v", err)
				pc.Close()
			}
			if err := p.Epoll.Add(targetConn); err != nil {
				logger.Errorf("failed to add connection %v", err)
				pc.Close()
			}
		}
	}()
}

func (p *Proxy) proxy(ep *utils.Epoll, ch chan *connData) {
	for {
		connections, err := ep.Wait()
		if err != nil {
			logger.Debugf("epoll failed to wait: %v", err)
			continue
		}
		for _, conn := range connections {
			if conn == nil {
				logger.Debugf("outbound conn nil")
				continue
			}

			connFd := utils.SocketFD(conn)

			var buf = make([]byte, 4096)
			n, readErr := conn.Read(buf)
			if readErr != nil {
				logger.Errorf("failed to read conn: %s, err: %v", conn.RemoteAddr().String(), readErr)
				if err := ep.Remove(conn); err != nil {
					logger.Errorf("failed to remove %v", err)
				}

				if tmp, ok := p.Connections.Load(connFd); ok {
					pc := tmp.(*proxyConn)
					if connFd == pc.InboundFd {
						if err := ep.Remove(pc.OutboundConn); err != nil {
							logger.Errorf("failed to remove %v", err)
						}
						pc.OutboundConn.Close()
					}
				}
				conn.Close()
				continue
			}

			cd := &connData{Fd: connFd, Data: buf[:n]}
			// logger.Debugf("read in, size: %d, %s", n, cd.String())
			ch <- cd
			logger.Debugf("conn active fd: %d, size: %d", connFd, n)
		}
	}
}

func (p *Proxy) Split(split SplitFunc) {
	p.split = split
}

func NewProxy(listenPort, backendAddress string) *Proxy {
	ep, err := utils.MkEpoll()
	if err != nil {
		logger.Errorf("failed to create epoll, err: %v", err)
		return nil
	}
	return &Proxy{ListenPort: listenPort, BackendAddress: backendAddress, Epoll: ep, Connections: &sync.Map{}}
}

func (p *Proxy) consume(ch chan *connData, connMap *sync.Map) {
	for {
		cd := <-ch
		// find conn
		conn, ok := connMap.Load(cd.Fd)
		if !ok {
			continue
		}
		pConn := conn.(*proxyConn)

		// append data to buffer
		pConn.appendBuf(cd.Fd, cd.Data)
		targetConn := pConn.GetTargetConn(cd.Fd)
		if targetConn == nil {
			logger.Warnf("target conn not found")
			continue
		}

		// scan
		for pConn.Scan(cd.Fd) {
			bytes := pConn.Bytes(cd.Fd)
			if len(bytes) == 0 {
				break
			}
			p.tokenHandler(bytes)
			// send token
			write, err := targetConn.Write(bytes)
			if err != nil {
				logger.Errorf("failed to write err: %v", err)
				return
			}
			logger.Debugf("write to conn: %s, bytes: %d", targetConn.RemoteAddr().String(), write)
		}
	}
	logger.Debugf("consume loop end.")
}

func (p *Proxy) TokenHandler(handler TokenHandlerFunc) {
	p.tokenHandler = handler
}
