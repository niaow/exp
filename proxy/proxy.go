package main

import (
	"context"
	"flag"
	"io"
	"log"
	"net"
	"sync"
)

func main() {
	var in string
	var out string
	flag.StringVar(&in, "in", ":80", "input port")
	flag.StringVar(&out, "out", "localhost:8080", "output port")
	flag.Parse()
	l, err := net.Listen("tcp", in)
	if err != nil {
		panic(err)
	}
	for {
		conn, err := l.Accept()
		if err != nil {
			log.Printf("failed to accept: %v", err)
		}
		go func() {
			dst, err := net.Dial("tcp", out)
			if err != nil {
				conn.Close()
				log.Printf("failed to create backend connection: %v", err)
			}
			spliceConn(conn, dst)
		}()
	}
}

func spliceConn(x, y net.Conn) {
	var once sync.Once
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-ctx.Done()
		x.Close()
		y.Close()
	}()
	go func() {
		defer cancel()
		_, err := io.Copy(x, y)
		if err != nil {
			once.Do(func() { log.Printf("connection lost: %v", err) })
		}
	}()
	go func() {
		defer cancel()
		_, err := io.Copy(y, x)
		if err != nil {
			once.Do(func() { log.Printf("connection lost: %v", err) })
		}
	}()
}
