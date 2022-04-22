package main

import (
	"encoding/hex"
	"flag"
	"fmt"
	"github.com/wiloon/w-tcp-proxy/utils"
	"github.com/wiloon/w-tcp-proxy/utils/logger"
	"net"
	"sync"
)

var (
	listenPort = flag.String("port", ":3000", "listening port")
	//targetAddress = flag.String("target-port", "127.0.0.1:3000", "target port")
	// targetAddress = flag.String("target-port", "192.168.122.1:22", "target port")
	targetAddress = flag.String("target-port", "192.168.122.1:22", "target port")
)

var epInbound *utils.Epoll
var epOutbound *utils.Epoll

type proxyConn struct {
	InboundFd    int
	InboundConn  net.Conn
	OutboundFd   int
	OutboundConn net.Conn
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

var proxyConnections = &sync.Map{}

type connData struct {
	Fd   int
	Data []byte
}

func (c *connData) String() string {
	return fmt.Sprintf("fd: %d, data: %s", c.Fd, hex.EncodeToString(c.Data))
}

func main() {
	flag.Parse()
	logger.InitTo(true, false, "debug", "foo")
	utils.SetNoFileLimit(10000, 20000)
	inboundListener, err := net.Listen("tcp", *listenPort)
	if err != nil {
		logger.Errorf("failed to listen %s", *listenPort, err)
		return
	}
	logger.Infof("proxy listening on %s", *listenPort)

	epInbound, err = utils.MkEpoll()
	epOutbound, err = utils.MkEpoll()

	//go inbound()
	//go outbound()
	ch0 := make(chan *connData, 100)
	ch1 := make(chan *connData, 100)

	go proxy(epInbound, ch0)
	go proxy(epOutbound, ch1)

	go consume(ch0, proxyConnections)
	go consume(ch1, proxyConnections)

	go func() {
		for {
			sourceConn, err := inboundListener.Accept()
			if err != nil {
				return
			}
			sourceConnFd := utils.SocketFD(sourceConn)
			logger.Infof("inbound conn: %s, fd: %d", sourceConn.RemoteAddr().String(), sourceConnFd)

			// dial target
			targetConn, err := net.Dial("tcp4", *targetAddress)
			if err != nil {
				logger.Errorf("failed to dial backend server %v", err)
				return
			}
			targetConnFd := utils.SocketFD(targetConn)
			logger.Infof("proxy, inbound: %s>%s, fd: %d, outbound: %s>%s, fd: %d",
				sourceConn.RemoteAddr().String(), sourceConn.LocalAddr().String(), sourceConnFd, targetConn.LocalAddr().String(), targetConn.RemoteAddr().String(), targetConnFd)

			pc := &proxyConn{InboundFd: sourceConnFd, InboundConn: sourceConn, OutboundFd: targetConnFd, OutboundConn: targetConn}
			proxyConnections.Store(sourceConnFd, pc)
			proxyConnections.Store(targetConnFd, pc)

			if err := epInbound.Add(sourceConn); err != nil {
				logger.Errorf("failed to add connection %v", err)
				sourceConn.Close()
			}
			if err := epOutbound.Add(targetConn); err != nil {
				logger.Errorf("failed to add connection %v", err)
				targetConn.Close()
			}
		}
	}()

	utils.WaitSignals(func() {
		logger.Infof("shutting down")
	})
}

func proxy(ep *utils.Epoll, ch chan *connData) {
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
			var buf = make([]byte, 0xffff)
			n, readErr := conn.Read(buf)
			if readErr != nil {
				logger.Errorf("failed to read conn: %s, err: %v", conn.RemoteAddr().String(), readErr)
				if err := ep.Remove(conn); err != nil {
					logger.Errorf("failed to remove %v", err)
				}
				conn.Close()
				break
			}
			connFd := utils.SocketFD(conn)
			logger.Debugf("read from conn: %d, size: %d", connFd, n)
			cd := &connData{Fd: connFd, Data: buf[:n]}
			//logger.Debugf("read in: %s", cd.String())
			ch <- cd
			// logger.Debugf("write to channel: %d, size: %d", connFd, n)
		}
	}
}

func consume(ch chan *connData, connMap *sync.Map) {
	for {
		cd := <-ch
		if tmpConn, ok := connMap.Load(cd.Fd); ok {
			pc := tmpConn.(*proxyConn)
			var targetConn net.Conn
			var targetFd int
			if cd.Fd == pc.InboundFd {
				targetConn = pc.OutboundConn
				targetFd = pc.OutboundFd
			} else {
				targetConn = pc.InboundConn
				targetFd = pc.InboundFd
			}
			write, err := targetConn.Write(cd.Data)
			if err != nil {
				logger.Errorf("failed to write :%v", err)
				return
			}
			logger.Debugf("write data %d>%d, size: %d", cd.Fd, targetFd, write)
			//logger.Debugf("write out: %s", cd.String())
		} else {
			logger.Warnf("target conn not found")
		}
	}
}
