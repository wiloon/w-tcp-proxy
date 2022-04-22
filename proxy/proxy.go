package proxy

import (
	"bufio"
	"github.com/wiloon/w-tcp-proxy/utils"
	"github.com/wiloon/w-tcp-proxy/utils/logger"
	"net"
	"sync"
)

type Proxy struct {
	ListenPort     string
	BackendAddress string
	Epoll          *utils.Epoll
	Connections    *sync.Map
	split          bufio.SplitFunc
}

type proxyConn struct {
	InboundFd       int
	InboundConn     net.Conn
	InboundScanner  *bufio.Scanner
	OutboundFd      int
	OutboundConn    net.Conn
	OutboundScanner *bufio.Scanner
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

type connData struct {
	Fd   int
	Data []byte
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

			pc := &proxyConn{InboundFd: sourceConnFd, InboundConn: sourceConn, OutboundFd: targetConnFd, OutboundConn: targetConn}
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

			cd := &connData{Fd: connFd, Data: nil}
			//logger.Debugf("read in: %s", cd.String())
			ch <- cd
			logger.Debugf("conn active fd: %d", connFd)
		}
	}
}

func (p *Proxy) Split(split bufio.SplitFunc) {
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
		if tmpConn, ok := connMap.Load(cd.Fd); ok {
			pc := tmpConn.(*proxyConn)

			var targetConn net.Conn
			var targetFd int

			if cd.Fd == pc.InboundFd {

				targetConn = pc.OutboundConn
				targetFd = pc.OutboundFd
				if pc.InboundScanner == nil {
					pc.InboundScanner = bufio.NewScanner(pc.InboundConn)
					pc.InboundScanner.Split(p.split)
				}
				for pc.InboundScanner.Scan() {
					stringProtocolBytes := pc.InboundScanner.Bytes()
					logger.Debugf("scan loop, data size: %d", len(stringProtocolBytes))
					if stringProtocolBytes == nil {
						continue
					}
					write, err := targetConn.Write(stringProtocolBytes)
					if err != nil {
						logger.Errorf("failed to write :%v", err)
						return
					}
					logger.Debugf("write data %d>%d, size: %d", cd.Fd, targetFd, write)
					//logger.Debugf("write out: %s", cd.String())
				}

			} else {
				targetConn = pc.InboundConn
				targetFd = pc.InboundFd
				if pc.OutboundScanner == nil {
					pc.OutboundScanner = bufio.NewScanner(pc.OutboundConn)
					pc.OutboundScanner.Split(p.split)
				}
				for pc.OutboundScanner.Scan() {
					stringProtocolBytes := pc.OutboundScanner.Bytes()
					if stringProtocolBytes == nil {
						continue
					}
					write, err := targetConn.Write(stringProtocolBytes)
					if err != nil {
						logger.Errorf("failed to write :%v", err)
						return
					}
					logger.Debugf("write data %d>%d, size: %d", cd.Fd, targetFd, write)
					//logger.Debugf("write out: %s", cd.String())
				}
			}

		} else {
			logger.Warnf("target conn not found")
		}
		logger.Debugf("consume loop end.")
	}
}
