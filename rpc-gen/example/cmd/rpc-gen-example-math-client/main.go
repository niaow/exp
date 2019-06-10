package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"strconv"

	"github.com/jadr2ddude/exp/rpc-gen/example/math"
)

func main() {
	var op string
	var x, y uint
	var num uint64
	var srv string
	var rawdat string
	flag.StringVar(&op, "op", "", "operation (add/divide/stats/sum/factor)")
	flag.UintVar(&x, "x", 1, "first argument")
	flag.UintVar(&y, "y", 1, "first argument")
	flag.Uint64Var(&num, "n", 1, "a big-ish number")
	flag.StringVar(&srv, "srv", "http://localhost:10000/", "server base URL")
	flag.StringVar(&rawdat, "dat", "", "comma-seperated data set")
	flag.Parse()
	var parsedDat []float64
	if rawdat != "" {
		err := json.Unmarshal([]byte("["+rawdat+"]"), &parsedDat)
		if err != nil {
			panic(err)
		}
	}
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
	case "stats":
		stats, err := cli.Statistics(context.Background(), parsedDat)
		if err != nil {
			panic(err)
		}
		fmt.Printf("Summary Statistics:\n\tMean:  %f\n\tStdev: %f\n", stats.Mean, stats.Stdev)
	case "sum":
		lines := bufio.NewScanner(os.Stdin)
		sum, err := cli.Sum(context.Background(), func() (float64, error) {
			if lines.Scan() {
				return strconv.ParseFloat(lines.Text(), 64)
			}
			err := lines.Err()
			if err != nil {
				return 0, err
			}
			return 0, io.EOF
		})
		if err != nil {
			panic(err)
		}
		fmt.Printf("Sum: %f\n", sum)
	case "factor":
		err := cli.Factor(context.Background(), num, func(f uint64) error {
			fmt.Printf("found prime factor: %d\n", f)
			return nil
		})
		if err != nil {
			panic(err)
		}
	default:
		panic(fmt.Errorf("unrecognized operation %q", op))
	}
}
