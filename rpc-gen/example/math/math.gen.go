package math

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
)

var _ = bytes.NewReader

// Math is a system to do math.
type Math interface {
	// Adds two numbers.
	// X is the first number.
	// Y is the second number.
	// Sum is the sum of the two numbers.
	Add(ctx context.Context, X uint32, Y uint32) (Sum uint32, err error)

	// Divides two numbers.
	// X is the dividend.
	// Y is the divisor.
	// Quotient is the quotient of the division.
	// Remainder is the remainder of the division.
	// May return ErrDivideByZero.
	Divide(ctx context.Context, X uint32, Y uint32) (Quotient uint32, Remainder uint32, err error)

	// Statistics calculates summative statistics for a set of data
	// Data is the data set to be summarized
	// Results are the resulting summary statistics.
	// May return ErrNoData.
	Statistics(ctx context.Context, Data []float64) (Results Stats, err error)
}

// Stats is a set of summative statistics.
type Stats struct {
	// Mean is the average of the data in the set
	Mean float64 `json:"Mean,omitempty"`

	// Stdev is the standard deviation of the data in the set
	Stdev float64 `json:"Stdev,omitempty"`
}

// ErrDivideByZero is an error resulting from a division with a zero divisor.
// This corresponds to the HTTP status code 400 "Bad Request".
type ErrDivideByZero struct {
	// Dividend is the dividend of the erroneous division.
	Dividend uint32 `json:"Dividend,omitempty"`
}

// ErrNoData is an error indicating that no data was provided to summarize.
// This corresponds to the HTTP status code 400 "Bad Request".
type ErrNoData struct{}

func (err ErrDivideByZero) Error() string {
	dat, merr := json.Marshal(err)
	if merr != nil {
		return "division by zero"
	}

	return fmt.Sprintf("%s (%s)", "division by zero", string(dat[1:len(dat)-1]))
}

func (err ErrNoData) Error() string {
	return "no data provided"
}

// rpcError is a container used to transmit errors across http.
type rpcError struct {
	Message string      `json:"message"`
	Type    string      `json:"type,omitempty"`
	Data    interface{} `json:"dat,omitempty"`
	Code    int         `json:"-"`
}

func (re rpcError) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	msg := re.Message
	if dat, err := json.Marshal(re); err == nil {
		msg = string(dat)
	}
	http.Error(w, msg, re.Code)
}

// ServeHTTP sends the error over HTTP.
func (err ErrDivideByZero) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rpcError{
		Message: err.Error(),
		Type:    "ErrDivideByZero",
		Data:    err,
		Code:    http.StatusBadRequest,
	}.ServeHTTP(w, r)
}

// ServeHTTP sends the error over HTTP.
func (err ErrNoData) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rpcError{
		Message: err.Error(),
		Type:    "ErrNoData",
		Data:    err,
		Code:    http.StatusBadRequest,
	}.ServeHTTP(w, r)
}

// httpMathHandler is a wrapper around Math that implements http.Handler.
type httpMathHandler struct {
	impl         Math
	ctxTransform func(context.Context, *http.Request) (context.Context, context.CancelFunc, error)
	mux          *http.ServeMux
}

// handleAdd wraps the implementation's Add operation and bridges it to HTTP.
func (h httpMathHandler) handleAdd(w http.ResponseWriter, r *http.Request) {
	var args struct {
		X uint32 `json:"X,omitempty"`
		Y uint32 `json:"Y,omitempty"`
	}

	if r.Method != http.MethodPost {
		rpcError{
			Message: fmt.Sprintf("unsupported method %q, please use %q", r.Method, http.MethodPost),
			Code:    http.StatusMethodNotAllowed,
		}.ServeHTTP(w, r)
		return
	}

	q := r.URL.Query()
	switch len(q["X"]) {
	case 0:
	case 1:
		if err := json.Unmarshal([]byte(q["X"][0]), &args.X); err != nil {
			rpcError{
				Message: err.Error(),
				Code:    http.StatusBadRequest,
			}.ServeHTTP(w, r)
			return
		}
	default:
		rpcError{
			Message: "argument \"X\" duplicated",
			Code:    http.StatusBadRequest,
		}.ServeHTTP(w, r)
		return
	}
	switch len(q["Y"]) {
	case 0:
	case 1:
		if err := json.Unmarshal([]byte(q["Y"][0]), &args.Y); err != nil {
			rpcError{
				Message: err.Error(),
				Code:    http.StatusBadRequest,
			}.ServeHTTP(w, r)
			return
		}
	default:
		rpcError{
			Message: "argument \"Y\" duplicated",
			Code:    http.StatusBadRequest,
		}.ServeHTTP(w, r)
		return
	}

	ctx := r.Context()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if h.ctxTransform != nil {
		tctx, tcancel, err := h.ctxTransform(ctx, r)
		if err != nil {
			rpcError{
				Message: err.Error(),
				Code:    http.StatusBadRequest,
			}.ServeHTTP(w, r)
			return
		}
		defer tcancel()
		ctx = tctx
	}

	var outputs struct {
		Sum uint32 `json:"Sum,omitempty"`
	}

	var err error
	outputs.Sum, err = h.impl.Add(ctx, args.X, args.Y)

	if err != nil {
		rpcError{
			Message: err.Error(),
			Code:    http.StatusInternalServerError,
		}.ServeHTTP(w, r)
		return
	}

	json.NewEncoder(w).Encode(outputs)
}

// handleDivide wraps the implementation's Divide operation and bridges it to HTTP.
func (h httpMathHandler) handleDivide(w http.ResponseWriter, r *http.Request) {
	var args struct {
		X uint32 `json:"X,omitempty"`
		Y uint32 `json:"Y,omitempty"`
	}

	if r.Method != http.MethodPost {
		rpcError{
			Message: fmt.Sprintf("unsupported method %q, please use %q", r.Method, http.MethodPost),
			Code:    http.StatusMethodNotAllowed,
		}.ServeHTTP(w, r)
		return
	}

	if err := json.NewDecoder(r.Body).Decode(&args); err != nil {
		rpcError{
			Message: err.Error(),
			Code:    http.StatusBadRequest,
		}.ServeHTTP(w, r)
		return
	}

	ctx := r.Context()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if h.ctxTransform != nil {
		tctx, tcancel, err := h.ctxTransform(ctx, r)
		if err != nil {
			rpcError{
				Message: err.Error(),
				Code:    http.StatusBadRequest,
			}.ServeHTTP(w, r)
			return
		}
		defer tcancel()
		ctx = tctx
	}

	var outputs struct {
		Quotient  uint32 `json:"Quotient,omitempty"`
		Remainder uint32 `json:"Remainder,omitempty"`
	}

	var err error
	outputs.Quotient, outputs.Remainder, err = h.impl.Divide(ctx, args.X, args.Y)

	if err != nil {
		switch e := err.(type) {
		case ErrDivideByZero:
			e.ServeHTTP(w, r)
		default:
			rpcError{
				Message: err.Error(),
				Code:    http.StatusInternalServerError,
			}.ServeHTTP(w, r)
		}
		return
	}

	json.NewEncoder(w).Encode(outputs)
}

// handleStatistics wraps the implementation's Statistics operation and bridges it to HTTP.
func (h httpMathHandler) handleStatistics(w http.ResponseWriter, r *http.Request) {
	var args struct {
		Data []float64 `json:"Data,omitempty"`
	}

	if r.Method != http.MethodPost {
		rpcError{
			Message: fmt.Sprintf("unsupported method %q, please use %q", r.Method, http.MethodPost),
			Code:    http.StatusMethodNotAllowed,
		}.ServeHTTP(w, r)
		return
	}

	if err := json.NewDecoder(r.Body).Decode(&args); err != nil {
		rpcError{
			Message: err.Error(),
			Code:    http.StatusBadRequest,
		}.ServeHTTP(w, r)
		return
	}

	ctx := r.Context()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if h.ctxTransform != nil {
		tctx, tcancel, err := h.ctxTransform(ctx, r)
		if err != nil {
			rpcError{
				Message: err.Error(),
				Code:    http.StatusBadRequest,
			}.ServeHTTP(w, r)
			return
		}
		defer tcancel()
		ctx = tctx
	}

	var outputs struct {
		Results Stats `json:"Results,omitempty"`
	}

	var err error
	outputs.Results, err = h.impl.Statistics(ctx, args.Data)

	if err != nil {
		switch e := err.(type) {
		case ErrNoData:
			e.ServeHTTP(w, r)
		default:
			rpcError{
				Message: err.Error(),
				Code:    http.StatusInternalServerError,
			}.ServeHTTP(w, r)
		}
		return
	}

	json.NewEncoder(w).Encode(outputs)
}

// ServeHTTP invokes the appropriate handler
func (h httpMathHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mux.ServeHTTP(w, r)
}

// NewHTTPMathHandler creates an http.Handler that wraps a Math.
// If not nil, ctxTransform will be called to transform the context with information from the HTTP request.
// If the ctxTransform returns an error, the error will be propogated to the client.
// The cancel function returned by ctxTransform will be invoked after the request completes.
func NewHTTPMathHandler(system Math, ctxTransform func(context.Context, *http.Request) (context.Context, context.CancelFunc, error)) http.Handler {
	mux := http.NewServeMux()
	h := &httpMathHandler{
		impl:         system,
		ctxTransform: ctxTransform,
		mux:          mux,
	}

	mux.HandleFunc("/Add", h.handleAdd)
	mux.HandleFunc("/Divide", h.handleDivide)
	mux.HandleFunc("/Statistics", h.handleStatistics)

	return h
}

// MathClient is an HTTP client for Math, implementing Math.
type MathClient struct {
	// HTTP is the HTTP client which will be used by the MathClient to make requests.
	HTTP *http.Client

	// Base is the base URL of the server.
	Base *url.URL

	// Contextualize is an optional callback that may be used to add contextual information to the HTTP request.
	// If Contextualize is not called, the parent context will be inserted into the request.
	// If present, the Contextualize callback is responsible for configuring request cancellation.
	Contextualize func(context.Context, *http.Request) (*http.Request, error)
}

// Adds two numbers.
// X is the first number.
// Y is the second number.
// Sum is the sum of the two numbers.
func (cli *MathClient) Add(ctx context.Context, X uint32, Y uint32) (Sum uint32, err error) {
	u, err := cli.Base.Parse("Add")
	if err != nil {
		return 0, err
	}

	q := u.Query()
	rawX, err := json.Marshal(X)
	if err != nil {
		return 0, err
	}
	q.Set("X", string(rawX))
	rawY, err := json.Marshal(Y)
	if err != nil {
		return 0, err
	}
	q.Set("Y", string(rawY))
	u.RawQuery = q.Encode()

	req, err := http.NewRequest(http.MethodPost, u.String(), nil)
	if err != nil {
		return 0, err
	}
	if cli.Contextualize == nil {
		req = req.WithContext(ctx)
	} else {
		cctx, cancel := context.WithCancel(ctx)
		defer cancel()

		req, err = cli.Contextualize(cctx, req)
		if err != nil {
			return 0, err
		}
	}

	hcl := cli.HTTP
	if hcl == nil {
		hcl = http.DefaultClient
	}
	resp, err := hcl.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		dat, eerr := ioutil.ReadAll(resp.Body)
		if eerr != nil {
			return 0, errors.New(resp.Status)
		}
		var rerr rpcError
		eerr = json.Unmarshal(dat, &rerr)
		if eerr != nil {
			return 0, errors.New(string(dat))
		}

		return 0, errors.New(rerr.Message)
	}

	bdat, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return 0, err
	}

	var outputs struct {
		Sum uint32 `json:"Sum,omitempty"`
	}
	err = json.Unmarshal(bdat, &outputs)
	if err != nil {
		return 0, err
	}

	return outputs.Sum, nil
}

// Divides two numbers.
// X is the dividend.
// Y is the divisor.
// Quotient is the quotient of the division.
// Remainder is the remainder of the division.
// May return ErrDivideByZero.
func (cli *MathClient) Divide(ctx context.Context, X uint32, Y uint32) (Quotient uint32, Remainder uint32, err error) {
	u, err := cli.Base.Parse("Divide")
	if err != nil {
		return 0, 0, err
	}

	dat, err := json.Marshal(struct {
		X uint32 `json:"X,omitempty"`
		Y uint32 `json:"Y,omitempty"`
	}{
		X: X,
		Y: Y,
	})
	if err != nil {
		return 0, 0, err
	}
	req, err := http.NewRequest(http.MethodPost, u.String(), bytes.NewReader(dat))
	if err != nil {
		return 0, 0, err
	}
	if cli.Contextualize == nil {
		req = req.WithContext(ctx)
	} else {
		cctx, cancel := context.WithCancel(ctx)
		defer cancel()

		req, err = cli.Contextualize(cctx, req)
		if err != nil {
			return 0, 0, err
		}
	}

	hcl := cli.HTTP
	if hcl == nil {
		hcl = http.DefaultClient
	}
	resp, err := hcl.Do(req)
	if err != nil {
		return 0, 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		dat, eerr := ioutil.ReadAll(resp.Body)
		if eerr != nil {
			return 0, 0, errors.New(resp.Status)
		}
		var rerr rpcError
		eerr = json.Unmarshal(dat, &rerr)
		if eerr != nil {
			return 0, 0, errors.New(string(dat))
		}

		rmsg := rerr.Message
		switch rerr.Type {
		case "ErrDivideByZero":
			rerr.Data = &ErrDivideByZero{}
		default:
			return 0, 0, errors.New(rmsg)
		}
		eerr = json.Unmarshal(dat, &rerr)
		if eerr != nil {
			return 0, 0, errors.New(rmsg)
		}
		decerr, ok := rerr.Data.(error)
		if !ok {
			return 0, 0, errors.New(rmsg)
		}
		return 0, 0, decerr
	}

	bdat, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return 0, 0, err
	}

	var outputs struct {
		Quotient  uint32 `json:"Quotient,omitempty"`
		Remainder uint32 `json:"Remainder,omitempty"`
	}
	err = json.Unmarshal(bdat, &outputs)
	if err != nil {
		return 0, 0, err
	}

	return outputs.Quotient, outputs.Remainder, nil
}

// Statistics calculates summative statistics for a set of data
// Data is the data set to be summarized
// Results are the resulting summary statistics.
// May return ErrNoData.
func (cli *MathClient) Statistics(ctx context.Context, Data []float64) (Results Stats, err error) {
	u, err := cli.Base.Parse("Statistics")
	if err != nil {
		return Stats{}, err
	}

	dat, err := json.Marshal(struct {
		Data []float64 `json:"Data,omitempty"`
	}{
		Data: Data,
	})
	if err != nil {
		return Stats{}, err
	}
	req, err := http.NewRequest(http.MethodPost, u.String(), bytes.NewReader(dat))
	if err != nil {
		return Stats{}, err
	}
	if cli.Contextualize == nil {
		req = req.WithContext(ctx)
	} else {
		cctx, cancel := context.WithCancel(ctx)
		defer cancel()

		req, err = cli.Contextualize(cctx, req)
		if err != nil {
			return Stats{}, err
		}
	}

	hcl := cli.HTTP
	if hcl == nil {
		hcl = http.DefaultClient
	}
	resp, err := hcl.Do(req)
	if err != nil {
		return Stats{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		dat, eerr := ioutil.ReadAll(resp.Body)
		if eerr != nil {
			return Stats{}, errors.New(resp.Status)
		}
		var rerr rpcError
		eerr = json.Unmarshal(dat, &rerr)
		if eerr != nil {
			return Stats{}, errors.New(string(dat))
		}

		rmsg := rerr.Message
		switch rerr.Type {
		case "ErrNoData":
			rerr.Data = &ErrNoData{}
		default:
			return Stats{}, errors.New(rmsg)
		}
		eerr = json.Unmarshal(dat, &rerr)
		if eerr != nil {
			return Stats{}, errors.New(rmsg)
		}
		decerr, ok := rerr.Data.(error)
		if !ok {
			return Stats{}, errors.New(rmsg)
		}
		return Stats{}, decerr
	}

	bdat, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return Stats{}, err
	}

	var outputs struct {
		Results Stats `json:"Results,omitempty"`
	}
	err = json.Unmarshal(bdat, &outputs)
	if err != nil {
		return Stats{}, err
	}

	return outputs.Results, nil
}
