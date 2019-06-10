package main

import (
	"context"
	"flag"
	"io"
	"net/http"

	gomath "math"

	"github.com/jadr2ddude/exp/rpc-gen/example/math"
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

func (m maff) Statistics(ctx context.Context, data []float64) (res math.Stats, err error) {
	if len(data) == 0 {
		return math.Stats{}, math.ErrNoData{}
	}

	var sum float64
	for _, v := range data {
		sum += v
	}
	mean := sum / float64(len(data))

	var sqerrs float64
	for _, v := range data {
		errv := v - mean
		sqerrs += errv * errv
	}
	stdev := gomath.Sqrt(sqerrs / float64(len(data)))

	return math.Stats{
		Mean:  mean,
		Stdev: stdev,
	}, nil
}

func (m maff) Sum(ctx context.Context, numbers func() (float64, error)) (float64, error) {
	res := 0.0
	for {
		v, err := numbers()
		if err != nil {
			if err == io.EOF {
				break
			}
			return 0.0, err
		}
		res += v
	}
	return res, nil
}

func (m maff) Factor(ctx context.Context, num uint64, factors func(uint64) error) error {
	for i := uint64(2); num != 1; i++ {
		if num%i == 0 {
			if err := factors(i); err != nil {
				return err
			}
			for num%i == 0 {
				num /= i
			}
		}
	}
	return nil
}
