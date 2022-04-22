package utils

import (
	"fmt"
	"github.com/wiloon/w-tcp-proxy/utils/logger"
	"syscall"
)

func _() syscall.Rlimit {
	var rLimit syscall.Rlimit
	err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &rLimit)
	if err != nil {
		logger.Errorf("Error Getting Rlimit ", err)
	}
	return rLimit
}

func SetNoFileLimit(cur, max uint64) {
	limit := syscall.Rlimit{Cur: cur, Max: max}
	err := syscall.Setrlimit(syscall.RLIMIT_NOFILE, &limit)
	if err != nil {
		fmt.Println("Error Setting Rlimit ", err)
	}
}
