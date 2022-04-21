package logger

import "testing"

const projectName = "test"

func Test0(t *testing.T) {
	// const projectName = "foo"
	// logger.Init("console,file", "debug", projectName)
	Init("console,file", "debug", projectName)

	Info("foo")
}
