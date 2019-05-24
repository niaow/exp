package main

import (
	"context"
	"flag"
	"net/http"

	"github.com/jadr2ddude/exp/rpc-gen/test/math"
)

func main() {
	var addr string
	flag.StringVar(&addr, "http", ":10000", "http server port")
	flag.Parse()
	http.ListenAndServe(addr, math.NewHTTPMathHandler(maff{}, nil))
}

type maff struct{}

func (m maff) Add(ctx context.Context, x uint32, y uint32) (sum uint32, err error) {
	return x + y, nil
}

func (m maff) Divide(ctx context.Context, x uint32, y uint32) (quotient uint32, remainder uint32, err error) {
	if y == 0 {
		return 0, 0, math.ErrDivideByZero{Dividend: x}
	}
	return x / y, x % y, nil
}
