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
	ListenPort      int
	ConnectionGroup sync.Map
	split           SplitFunc
	tokenHandler    TokenHandlerFunc
	Route           *Route
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

var connMap = make(map[int]Route)
var addressFd = make(map[string]int)

func (p *Proxy) Start() {
	utils.SetNoFileLimit(10000, 20000)
	inboundListener, err := net.Listen("tcp", fmt.Sprintf(":%d", p.ListenPort))
	if err != nil {
		logger.Errorf("failed to listen: %s, err: %v", p.ListenPort, err)
		return
	}
	logger.Infof("proxy listening on: %d", p.ListenPort)

	ch0 := make(chan *connData, 100)

	go p.epWait(ch0)

	go p.consume(ch0, p.Connections)

	go func() {
		for {
			inboundConn, err := inboundListener.Accept()
			if err != nil {
				return
			}
			go p.inboundConnHandler(inboundConn)
		}
	}()
}

func (p *Proxy) epWait(ch chan *connData) {
	for {
		connections, err := Ep.Wait()
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
				// close connections
				logger.Errorf("failed to read conn: %s, err: %v", conn.RemoteAddr().String(), readErr)
				p.CloseConn(connFd)

				c, exist := proxyConnections.Load(connFd)
				if !exist {
					logger.Warnf("conn not found")
				}
				cc := c.(Connection)
				if !cc.Backend {
					// inbound conn
					// close all conn

				}

				if err := Ep.Remove(conn); err != nil {
					logger.Errorf("failed to remove %v", err)
				}
				logger.Infof("remove conn from epoll: %s<>%s", conn.LocalAddr().String(), conn.RemoteAddr().String())

				if tmp, ok := p.Connections.Load(connFd); ok {
					pc := tmp.(*proxyGroup)
					if connFd == pc.BackendMainConn {
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

func (p *Proxy) BindRoute(r *Route) {
	p.Route = r
	// p.Route.InitBackendConn(p.split)
}

func NewProxy(listenPort int) *Proxy {
	return &Proxy{
		ListenPort:  listenPort,
		Connections: &sync.Map{},
	}
}

func (p *Proxy) consume(ch chan *connData, connMap *sync.Map) {
	for {
		cd := <-ch
		fd := cd.Fd
		logger.Debugf("consume fd: %d", fd)

		// append data to buffer
		c, ok := proxyConnections.Load(cd.Fd)
		if !ok {
			logger.Warnf("conn not dound: %d", cd.Fd)
			continue
		}
		pConn := c.(Connection)
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
			if r, ok := p.Route.Rules.Load(string(key)); ok {
				rule := r.(Rule)
				for _, backendConnConfig := range rule.Backends {
					pc, ok := p.ConnectionGroup.Load(backendConnConfig.RouteId)
					if !ok {
						logger.Errorf("route id not found: %d", backendConnConfig.RouteId)
					}
					pcc := pc.(Connection)
					c, exist := proxyConnections.Load(pcc.Fd)
					if !exist {
						logger.Warnf("conn not found: %d", pcc.Fd)
						continue
					}
					backendConn := c.(Connection)
					logger.Debugf("send to backend: %+v", backendConn)
					backendConn.Conn.Write(data)
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

const routeIdInbound = "-1"

func (p *Proxy) inboundConnHandler(inboundConn net.Conn) {
	inboundFd := utils.SocketFD(inboundConn)
	logger.Infof("inbound conn: fd: %d, %s<>%s", inboundFd, inboundConn.LocalAddr().String(), inboundConn.RemoteAddr().String())

	scanner := NewScanner(make([]byte, 4096))
	scanner.split = p.split

	pc := &Connection{
		Conn:    inboundConn,
		Fd:      inboundFd,
		RouteId: routeIdInbound,
		Address: inboundConn.RemoteAddr().String(),
		Scanner: scanner,
	}
	proxyConnections.Store(
		inboundFd,
		pc,
	)
	p.ConnectionGroup.Store(pc.RouteId, pc)
	// create backend conn, for all backend conn
	for k, ConnConfig := range p.Route.backends {
		// todo retry
		backendConn, err := net.Dial("tcp4", v.Address)
		if err != nil {
			logger.Errorf("failed to dial backend server: %v", err)
			return
		}
		fd := utils.SocketFD(backendConn)
		scanner := NewScanner(make([]byte, 4096))
		scanner.split = p.split

		bpc := &Connection{
			Conn:    backendConn,
			Fd:      fd,
			RouteId: ConnConfig.RouteId,
			Address: inboundConn.RemoteAddr().String(),
			Scanner: scanner,
		}
		proxyConnections.Store(
			fd,
			bpc,
		)
		p.ConnectionGroup.Store(bpc.RouteId, bpc)
		if err := Ep.Add(backendConn); err != nil {
			logger.Errorf("failed to add connection %v", err)
			// todo close conn peer
		}
		logger.Infof("backend backendConn created, id: %v", k)
	}
	if err := Ep.Add(inboundConn); err != nil {
		logger.Errorf("failed to add connection %v", err)
		// todo close conn peer
	}
}

func (p *Proxy) CloseConn(fd int) {
	// get conn by fd
	c, ok := proxyConnections.Load(fd)
	if !ok {
		logger.Warnf("conn not found fd: %d", fd)
		return
	}
	cc := c.(Connection)
	if cc.Backend == false || (cc.Backend == true && cc.Default == true) {
		//  if inbound conn, close all
		// if default backend conn, close all
		p.ConnectionGroup.Range(func(key, value any) bool {
			v := value.(Connection)
			// remove from epoll
			Ep.Remove(v.Conn)
			// close conn
			v.Conn.Close()
			return true
		})

	} else if cc.Backend == true && cc.Default == false {
		// if other backend conn,
		//		if forward mode, forward data to default backend, update route
		// mock login
		// keep conn, copy heart beat
		p.ConnectionGroup.Range(func(key, value any) bool {
			if value.(Connection).Default {
				p.Route.UpdateRule(value.(Connection))
			}
			return true
		})
	}

	//		if copy mode, retry, do nothing

}
