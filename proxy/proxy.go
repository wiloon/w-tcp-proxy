package proxy

import (
	"encoding/hex"
	"fmt"
	"github.com/wiloon/w-tcp-proxy/config"
	"github.com/wiloon/w-tcp-proxy/utils"
	"github.com/wiloon/w-tcp-proxy/utils/logger"
	"net"
	"sync"
)

type TokenHandlerFunc func(token []byte)

var epoll *utils.Epoll

type Proxy struct {
	ListenPort int

	Connections  *sync.Map
	split        SplitFunc
	tokenHandler TokenHandlerFunc
}

type BackendServer struct {
	Address string
	Default bool
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

func (c proxyConnection) Dial() {
	// create conn to all backend
	conn, err := net.Dial("tcp4", c.Address)
	if err != nil {
		logger.Errorf("failed to dial backend server %v", err)
		return
	}
	fd := utils.SocketFD(conn)

	pc := proxyConnection{
		Fd:      fd,
		Conn:    conn,
		Scanner: NewScanner(make([]byte, 4096)),
	}
	pc.Scanner.Split(split)

	proxyConnections[fd] = &pc

	if err := epoll.Add(conn); err != nil {
		logger.Errorf("failed to add connection %v", err)
		// todo close conn peer
	}
	addressFd[c.Address] = fd
	logger.Infof("backend conn, fd: %d, %s<>%s",
		fd, conn.LocalAddr().String(), conn.RemoteAddr().String())
}

type proxyGroup struct {
	InboundConn           *proxyConnection
	BackendMainConn       *proxyConnection
	BackendReplicatorConn *proxyConnection
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

func (pc *proxyGroup) GetConnFd(fd int) *proxyConnection {
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

type Route struct {
	source int
	target []int
}

var connMap = make(map[int]Route)
var addressFd = make(map[string]int)

func (p *Proxy) Start() {
	var err error
	Epoll, err = utils.MkEpoll()
	utils.SetNoFileLimit(10000, 20000)
	inboundListener, err := net.Listen("tcp", fmt.Sprintf(":%d", p.ListenPort))
	if err != nil {
		logger.Errorf("failed to listen: %s, err: %v", p.ListenPort, err)
		return
	}
	logger.Infof("proxy listening on: %d", p.ListenPort)

	ch0 := make(chan *connData, 100)

	go p.epWait(p.Epoll, ch0)

	go p.consume(ch0, p.Connections)

	go func() {
		for {
			inboundConn, err := inboundListener.Accept()
			if err != nil {
				return
			}
			inboundFd := utils.SocketFD(inboundConn)
			logger.Infof("inbound conn: fd: %d, %s<>%s", inboundFd, inboundConn.LocalAddr().String(), inboundConn.RemoteAddr().String())

			proxyConnections[inboundFd] = NewProxyConn(inboundFd, inboundConn, p.split, "")
			if err := p.Epoll.Add(inboundConn); err != nil {
				logger.Errorf("failed to add connection %v", err)
				// todo close conn peer
			}

			var targets []int
			for _, v := range config.Instance.Backends {
				// create conn to all backend
				conn, err := net.Dial("tcp4", v.Address)
				if err != nil {
					logger.Errorf("failed to dial backend server %v", err)
					return
				}
				fd := utils.SocketFD(conn)
				proxyConnections[fd] = NewProxyConn(fd, conn, p.split, "")
				targets = append(targets, fd)
				connMap[fd] = Route{source: fd, target: []int{inboundFd}}

				if err := p.Epoll.Add(conn); err != nil {
					logger.Errorf("failed to add connection %v", err)
					// todo close conn peer
				}
				addressFd[v.Address] = fd
				logger.Infof("backend conn, fd: %d, %s<>%s",
					fd, conn.LocalAddr().String(), conn.RemoteAddr().String())
			}
			connMap[inboundFd] = Route{source: inboundFd, target: targets}
		}
	}()
}

func (p *Proxy) epWait(ep *utils.Epoll, ch chan *connData) {
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
			logger.Debugf("conn active, fd: %d, data size: %d", connFd, n)
		}
	}
}

func (p *Proxy) Split(split SplitFunc) {
	p.split = split
}

func NewProxy(listenPort int, r *sync.Map) *Proxy {

	if err != nil {
		logger.Errorf("failed to create epoll, err: %v", err)
		return nil
	}
	return &Proxy{
		ListenPort:  listenPort,
		Epoll:       ep,
		Connections: &sync.Map{},
	}
}

func (p *Proxy) consume(ch chan *connData, connMap *sync.Map) {
	for {
		cd := <-ch
		fd := cd.Fd
		logger.Debugf("consume fd: %d", fd)

		// append data to buffer
		pConn := proxyConnections[cd.Fd]
		pConn.Scanner.appendBuf(cd.Data)
		//pConn.appendBuf(cd.Fd, cd.Data)

		for pConn.Scanner.Scan() {
			key := pConn.Scanner.Key()
			data := pConn.Scanner.Bytes()
			logger.Debugf("scan key: %s, data: %s", string(key), string(data))
			if len(data) == 0 {
				break
			}
			// get backend by key
			if r, ok := RuleMap.Load(string(key)); ok {
				rule := r.(Rule)
				for _, address := range rule.Backends {
					connFd := addressFd[address]
					bConn := proxyConnections[connFd]
					logger.Debugf("send to backend, address: %s, fd: %d, b conn: %v", address, connFd, bConn)
					bConn.Conn.Write(data)
				}
			}
		}
	}
	// logger.Debug("consume loop end.")
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
