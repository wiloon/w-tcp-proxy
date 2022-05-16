package proxy

import (
	"fmt"
	"testing"
)

func Test(t *testing.T) {
	buf := make([]byte, 4096)
	s := NewScanner(buf)
	s.split = split0

	s.appendBuf([]byte("7fd6cbd0-b43f-4615-9161-e5305b7dc43b"))
	for s.Scan() {
		data := string(s.Bytes())
		fmt.Println(data)
	}

	s.appendBuf([]byte("ca3aeb81-089e-43cc-9444-d36747181b68"))
	for s.Scan() {
		data := string(s.Bytes())
		fmt.Println(data)
	}
}
func split0(data []byte, eof bool) (advance int, key,token []byte, err error) {
	token = data
	key=[]byte("key0")
	return len(data), key,token, err
}
