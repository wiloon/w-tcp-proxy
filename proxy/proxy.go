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
	ListenPort            string
	BackendMain           string
	BackendReplicator     string
	BackendReplicatorMode string
	Epoll                 *utils.Epoll
	Connections           *sync.Map
	split                 SplitFunc
	tokenHandler          TokenHandlerFunc
}

type proxyConnection struct {
	Fd      int
	Conn    net.Conn
	Scanner *Scanner
	Mode    string
}

const proxyModeCopy = "copy"
const proxyModeForward = "forward"

func (c proxyConnection) isForwardMode() bool {
	if c.Mode == proxyModeForward {
		return true
	}
	return false
}

func (c proxyConnection) isCopyMode() bool {
	if c.Mode == proxyModeCopy {
		return true
	}
	return false
}

func NewProxyConn(fd int, conn net.Conn, split SplitFunc, mode string) proxyConnection {
	pc := proxyConnection{
		Fd:      fd,
		Conn:    conn,
		Scanner: NewScanner(make([]byte, 4096)),
		Mode:    mode,
	}
	pc.Scanner.Split(split)
	return pc
}

type proxyGroup struct {
	InboundConn           proxyConnection
	BackendMainConn       proxyConnection
	BackendReplicatorConn proxyConnection
}

func (pc *proxyGroup) Close() {
	err := pc.InboundConn.Conn.Close()
	if err != nil {
		logger.Errorf("failed to close conn: %v", pc.InboundConn.Conn.RemoteAddr().String())
	}
	err = pc.BackendMainConn.Conn.Close()
	if err != nil {
		logger.Errorf("failed to close conn: %v", pc.BackendMainConn.Conn.RemoteAddr().String())
	}
	err = pc.BackendReplicatorConn.Conn.Close()
	if err != nil {
		logger.Errorf("failed to close conn: %v", pc.BackendReplicatorConn.Conn.RemoteAddr().String())
	}
}

func (pc *proxyGroup) appendBuf(fd int, data []byte) {
	if fd == pc.InboundConn.Fd {
		pc.InboundConn.Scanner.appendBuf(data)
	} else if fd == pc.BackendMainConn.Fd {
		pc.BackendMainConn.Scanner.appendBuf(data)
	} else {
		if pc.BackendReplicatorConn.isForwardMode() {
			pc.BackendReplicatorConn.Scanner.appendBuf(data)
		}
	}
}

func (pc *proxyGroup) isCopyMode(fd int) bool {
	if pc.BackendReplicatorConn.isCopyMode() {
		return true
	}
	return false
}

func (pc *proxyGroup) send(fd int, data []byte) {
	conn := pc.GetConnFd(fd)
	// scan
	for conn.Scanner.Scan() {
		bytes := conn.Scanner.Bytes()
		if len(bytes) == 0 {
			break
		}
		if pc.isInboundConn(fd) {
			if pc.isForwardMode(fd) {
				pc.BackendReplicatorConn.Conn.Write(bytes)
			} else if pc.isCopyMode(fd) {
				mn, err := pc.BackendMainConn.Conn.Write(bytes)
				if err != nil {
					logger.Errorf("failed to write err: %v", err)
					return
				}
				logger.Debugf("write to conn: %s, bytes: %d", pc.BackendMainConn.Conn.RemoteAddr().String(), mn)
				rn, err := pc.BackendReplicatorConn.Conn.Write(bytes)
				if err != nil {
					logger.Errorf("failed to write err: %v", err)
					return
				}
				logger.Debugf("write to conn: %s, bytes: %d", pc.BackendReplicatorConn.Conn.RemoteAddr().String(), rn)
			}
		} else if pc.isBackendMain(fd) {
			conn := pc.InboundConn.Conn
			mn, err := conn.Write(bytes)
			if err != nil {
				logger.Errorf("failed to write err: %v", err)
				return
			}
			logger.Debugf("write to conn: %s, bytes: %d", conn.RemoteAddr().String(), mn)
		} else if pc.isReplicator(fd) {
			if pc.isForwardMode(fd) {
				pc.BackendReplicatorConn.Conn.Write(bytes)
			} else if pc.isCopyMode(fd) {
				mn, err := pc.BackendMainConn.Conn.Write(bytes)
				if err != nil {
					logger.Errorf("failed to write err: %v", err)
					return
				}
				logger.Debugf("write to conn: %s, bytes: %d", pc.BackendMainConn.Conn.RemoteAddr().String(), mn)
				rn, err := pc.BackendReplicatorConn.Conn.Write(bytes)
				if err != nil {
					logger.Errorf("failed to write err: %v", err)
					return
				}
				logger.Debugf("write to conn: %s, bytes: %d", pc.BackendReplicatorConn.Conn.RemoteAddr().String(), rn)
			}
		}
	}
}

func (pc *proxyGroup) GetConnFd(fd int) proxyConnection {
	if fd == pc.InboundConn.Fd {
		return pc.InboundConn
	} else if fd == pc.BackendMainConn.Fd {
		return pc.BackendMainConn
	} else {
		return pc.BackendReplicatorConn
	}
}

func (pc *proxyGroup) isForwardMode(fd int) bool {
	if fd == pc.BackendReplicatorConn.Fd && pc.BackendReplicatorConn.isForwardMode() {
		return true
	}
	return false
}

func (pc *proxyGroup) isReplicator(fd int) bool {
	if fd == pc.BackendReplicatorConn.Fd {
		return true
	}
	return false
}

func (pc *proxyGroup) isInboundConn(fd int) bool {
	if fd == pc.InboundConn.Fd {
		return true
	}
	return false
}

func (pc *proxyGroup) isBackendMain(fd int) bool {
	if fd == pc.BackendMainConn.Fd {
		return true
	}
	return false
}

type connData struct {
	Fd   int
	Data []byte
}

func (c *connData) String() string {
	return fmt.Sprintf("fd: %d, data: %s", c.Fd, hex.EncodeToString(c.Data))
}

func (p *Proxy) Start() {
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
			logger.Infof("inbound conn: fd: %d, %s<>%s", sourceConnFd, sourceConn.LocalAddr().String(), sourceConn.RemoteAddr().String())

			// dial target
			backendMainConn, err := net.Dial("tcp4", p.BackendMain)
			if err != nil {
				logger.Errorf("failed to dial backend server %v", err)
				return
			}
			backendMainFd := utils.SocketFD(backendMainConn)
			logger.Infof("backend conn main, fd: %d, %s<>%s",
				backendMainFd, backendMainConn.LocalAddr().String(), backendMainConn.RemoteAddr().String())

			backendReplicatorConn, err := net.Dial("tcp4", p.BackendReplicator)
			if err != nil {
				logger.Errorf("failed to dial backend server %v", err)
				return
			}
			backendReplicatorFd := utils.SocketFD(backendReplicatorConn)
			logger.Infof("backend conn replicator, fd: %d, %s<>%s",
				backendReplicatorFd, backendReplicatorConn.LocalAddr().String(), backendReplicatorConn.RemoteAddr().String())

			pc := &proxyGroup{
				InboundConn:           NewProxyConn(sourceConnFd, sourceConn, p.split, ""), // todo, inbound conn no mode
				BackendMainConn:       NewProxyConn(backendMainFd, backendMainConn, p.split, ""),
				BackendReplicatorConn: NewProxyConn(backendReplicatorFd, backendReplicatorConn, p.split, p.BackendReplicatorMode),
			}

			p.Connections.Store(sourceConnFd, pc)
			p.Connections.Store(backendMainFd, pc)
			p.Connections.Store(backendReplicatorFd, pc)

			if err := p.Epoll.Add(sourceConn); err != nil {
				logger.Errorf("failed to add connection %v", err)
				pc.Close()
			}
			if err := p.Epoll.Add(backendMainConn); err != nil {
				logger.Errorf("failed to add connection %v", err)
				pc.Close()
			}
			if err := p.Epoll.Add(backendReplicatorConn); err != nil {
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
				logger.Infof("remove conn from epoll: %s<>%s", conn.LocalAddr().String(), conn.RemoteAddr().String())

				if tmp, ok := p.Connections.Load(connFd); ok {
					pc := tmp.(*proxyGroup)
					if connFd == pc.InboundConn.Fd {
						// close all
						removeConnFromEp(ep, pc.BackendMainConn.Conn)
						pc.BackendMainConn.Conn.Close()
						logger.Infof("close conn: %s<>%s", pc.BackendMainConn.Conn.LocalAddr().String(), pc.BackendMainConn.Conn.RemoteAddr().String())
						removeConnFromEp(ep, pc.BackendReplicatorConn.Conn)
						pc.BackendReplicatorConn.Conn.Close()
						logger.Infof("close conn: %s<>%s", pc.BackendReplicatorConn.Conn.LocalAddr().String(), pc.BackendReplicatorConn.Conn.RemoteAddr().String())
					} else if connFd == pc.BackendMainConn.Fd { // todo , one backend close, check mode, and active conn
						if pc.isCopyMode(connFd) {
							// close all
							removeConnFromEp(ep, pc.InboundConn.Conn)
							pc.InboundConn.Conn.Close()
							logger.Infof("close conn: %s<>%s", pc.InboundConn.Conn.LocalAddr().String(), pc.InboundConn.Conn.RemoteAddr().String())
							removeConnFromEp(ep, pc.BackendMainConn.Conn)
							pc.BackendMainConn.Conn.Close()
							logger.Infof("close conn: %s<>%s", pc.BackendMainConn.Conn.LocalAddr().String(), pc.BackendMainConn.Conn.RemoteAddr().String())
							removeConnFromEp(ep, pc.BackendReplicatorConn.Conn)
							pc.BackendReplicatorConn.Conn.Close()
							logger.Infof("close conn: %s<>%s", pc.BackendReplicatorConn.Conn.LocalAddr().String(), pc.BackendReplicatorConn.Conn.RemoteAddr().String())
						} else {
							//todo
						}
					} else if connFd == pc.BackendReplicatorConn.Fd {
						// do nothing
					}
				}
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

func NewProxy(listenPort, backendMain, backendReplicator, backendReplicatorMode string) *Proxy {
	ep, err := utils.MkEpoll()
	if err != nil {
		logger.Errorf("failed to create epoll, err: %v", err)
		return nil
	}
	return &Proxy{
		ListenPort:            listenPort,
		BackendMain:           backendMain,
		BackendReplicator:     backendReplicator,
		BackendReplicatorMode: backendReplicatorMode,
		Epoll:                 ep,
		Connections:           &sync.Map{},
	}
}

func (p *Proxy) consume(ch chan *connData, connMap *sync.Map) {
	for {
		cd := <-ch
		fd := cd.Fd
		logger.Debugf("consume fd: %d", fd)
		// find conn
		conn, ok := connMap.Load(fd)
		if !ok {
			continue
		}
		pConn := conn.(*proxyGroup)
		if pConn.isCopyMode(fd) && pConn.isReplicator(fd) {
			continue
		}

		// append data to buffer
		pConn.appendBuf(cd.Fd, cd.Data)
		pConn.send(cd.Fd, cd.Data)

	}
	logger.Debugf("consume loop end.")
}

func (p *Proxy) TokenHandler(handler TokenHandlerFunc) {
	p.tokenHandler = handler
}

func removeConnFromEp(ep *utils.Epoll, conn net.Conn) {
	if err := ep.Remove(conn); err != nil {
		logger.Errorf("failed to remove %v", err)
	}
	logger.Infof("remove conn from epoll: %s<>%s", conn.LocalAddr().String(), conn.RemoteAddr().String())
}
