package proxy

import (
	"github.com/wiloon/w-tcp-proxy/utils"
	"github.com/wiloon/w-tcp-proxy/utils/logger"
	"golang.org/x/sys/unix"
	"net"
	"sync"
	"syscall"
)

var Ep *Epoll

func init() {
	var err error
	Ep, err = MkEpoll()
	if err != nil {
		logger.Errorf("failed to create epoll, err: %v", err)
		panic("epoll error")
	}

}

type Epoll struct {
	Fd          int
	Connections map[int]net.Conn
	Lock        *sync.RWMutex
}

func MkEpoll() (*Epoll, error) {
	fd, err := unix.EpollCreate1(0)
	if err != nil {
		return nil, err
	}
	return &Epoll{
		Fd:          fd,
		Lock:        &sync.RWMutex{},
		Connections: make(map[int]net.Conn),
	}, nil
}

func (e *Epoll) Add(conn net.Conn) error {
	// Extract file descriptor associated with the connection
	fd := utils.SocketFD(conn)
	err := unix.EpollCtl(e.Fd, syscall.EPOLL_CTL_ADD, fd, &unix.EpollEvent{Events: unix.POLLIN | unix.POLLHUP, Fd: int32(fd)})
	if err != nil {
		return err
	}
	e.Lock.Lock()
	defer e.Lock.Unlock()
	e.Connections[fd] = conn
	if len(e.Connections)%100 == 0 {
		logger.Infof("total number of connections: %v", len(e.Connections))
	}
	return nil
}
func (e *Epoll) Remove(conn net.Conn) error {
	fd := utils.SocketFD(conn)
	err := unix.EpollCtl(e.Fd, syscall.EPOLL_CTL_DEL, fd, nil)
	if err != nil {
		logger.Errorf("failed to delete fd: %d, err: %v", fd, err)
		return err
	}
	e.Lock.Lock()
	defer e.Lock.Unlock()
	delete(e.Connections, fd)
	if len(e.Connections)%100 == 0 {
		logger.Infof("total number of connections: %v", len(e.Connections))
	}
	return nil
}
func (e *Epoll) Wait() ([]net.Conn, error) {
	events := make([]unix.EpollEvent, 100)
	n, err := unix.EpollWait(e.Fd, events, 100)
	if err != nil {
		return nil, err
	}
	e.Lock.RLock()
	defer e.Lock.RUnlock()
	var connections []net.Conn
	for i := 0; i < n; i++ {
		conn := e.Connections[int(events[i].Fd)]
		connections = append(connections, conn)
	}
	return connections, nil
}
