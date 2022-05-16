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


type connData struct {
	Fd   int
	Data []byte
}

func (c *connData) String() string {
	return fmt.Sprintf("fd: %d, data: %s", c.Fd, hex.EncodeToString(c.Data))
}

func (p *Proxy) Start() {

	// set file limit
	utils.SetNoFileLimit(10000, 20000)

	ch0 := make(chan *connData, 100)
	go p.epWait(ch0)
	go p.consume(ch0)

	inboundListener, err := net.Listen("tcp", fmt.Sprintf(":%d", p.ListenPort))
	if err != nil {
		logger.Errorf("failed to listen: %s, err: %v", p.ListenPort, err)
		return
	}
	logger.Infof("proxy listening on: %d", p.ListenPort)
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
		ListenPort: listenPort,
	}
}

func (p *Proxy) consume(ch chan *connData) {
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
		backendConn, err := net.Dial("tcp4", ConnConfig.Address)
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
	if !cc.Backend || (cc.Backend && cc.Default) {
		// if inbound conn, close all
		// if default backend conn, close all
		p.ConnectionGroup.Range(func(key, value any) bool {
			v := value.(Connection)
			// remove from epoll
			Ep.Remove(v.Conn)
			// close conn
			v.Conn.Close()
			return true
		})

	} else if cc.Backend && !cc.Default {
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
