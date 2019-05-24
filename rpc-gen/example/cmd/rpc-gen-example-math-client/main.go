package main

import (
	"context"
	"flag"
	"fmt"
	"net/url"

	"github.com/jadr2ddude/exp/rpc-gen/example/math"
)

func main() {
	var op string
	var x, y uint
	var srv string
	flag.StringVar(&op, "op", "", "operation (add or divide)")
	flag.UintVar(&x, "x", 1, "first argument")
	flag.UintVar(&y, "y", 1, "first argument")
	flag.StringVar(&srv, "srv", "http://localhost:10000/", "server base URL")
	flag.Parse()
	u, err := url.Parse(srv)
	if err != nil {
		panic(err)
	}
	cli := math.MathClient{Base: u}
	switch op {
	case "add":
		sum, err := cli.Add(context.Background(), uint32(x), uint32(y))
		if err != nil {
			panic(err)
		}
		fmt.Println(sum)
	case "divide":
		quo, remainder, err := cli.Divide(context.Background(), uint32(x), uint32(y))
		if err != nil {
			panic(err)
		}
		fmt.Println(quo, remainder)
	}
}
