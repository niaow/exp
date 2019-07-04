// +build go1.12

package ws

import (
	"bufio"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// HandshakeOptions is a set of options for a websocket handshake.
type HandshakeOptions struct {
	SupportedProtocols []string
	Headers            http.Header

	// PingInterval is the interval at which pings are normally sent.
	// Defaults to 30 seconds.
	PingInterval time.Duration

	// PongTimeout is the maximum duration between the sending of a ping and reception of a pong.
	// This should be a multiple of PingInterval, otherwise it will be rounded up to a multiple of PingInterval.
	// Defaults to 2*PingInterval.
	PongTimeout time.Duration
}

// Handshake is metadata from a websocket handshake.
type Handshake struct {
	// Method is the HTTP method used to establish the connection.
	// It is either http.MethodGet or http.MethodConnect.
	// The http.MethodGet corressponds to HTTP/1 whereas http.MethodConnect corressponds to HTTP/2.
	// NOTE: it is possible for http.MethodConnect to be used for HTTP/1.
	Method string

	// HTTPMajor and HTTPMinor are the version numbers of the HTTP protocol used.
	HTTPMajor, HTTPMinor int

	// Version is the version number of the websocket protocol used.
	Version int

	// Protocol is the selected websocket protocol.
	Protocol string
}

// A dialer contains options for connecting over websocket.
type Dialer struct {
	// HTTPClient is the http client that will be used for connections.
	// Required.
	HTTPClient *http.Client

	// When DisableHTTP1 is true, the HTTP/1 websockets will never be created.
	DisableHTTP1 bool

	// When DisableHTTP2 is true, the HTTP/2 websockets will never be created.
	DisableHTTP2 bool

	// If PreferHTTP1 is true, HTTP/1 websockets will be tried first, then HTTP/2 will be tried next.
	// Otherwise, HTTP/2 will be tried first, then HTTP/2 will be tried next.
	// The secondary will be tried only if the first fails with an HTTP 405 "Method Not Allowed".
	PreferHTTP1 bool

	// Rand is the source of random data for challenges.
	// Required.
	Rand io.Reader
}

func (d *Dialer) challenge() (string, error) {
	dat := make([]byte, 16)
	_, err := io.ReadFull(d.Rand, dat)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(dat), nil
}

func challengeResponse(req *http.Request) string {
	hash := sha1.Sum(
		[]byte(
			req.Header.Get("Sec-WebSocket-Key") +
				"258EAFA5-E914-47DA-95CA-C5AB0DC85B11",
		),
	)
	return base64.StdEncoding.EncodeToString(hash[:])
}

func (d *Dialer) dialHTTP1(ctx context.Context, u *url.URL, opts HandshakeOptions) (*Conn, Handshake, error) {
	// create request
	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, Handshake{}, err
	}

	// generate headers
	if len(opts.Headers) > 0 {
		req.Header = opts.Headers
	}
	ch, err := d.challenge()
	if err != nil {
		return nil, Handshake{}, err
	}
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Sec-WebSocket-Key", ch)
	req.Header.Set("Sec-WebSocket-Version", "13")
	if len(opts.SupportedProtocols) > 0 {
		for _, v := range opts.SupportedProtocols {
			for _, c := range []rune(v) {
				switch {
				case c >= 'a' && c <= 'z':
				case c >= 'A' && c <= 'Z':
				default:
					return nil, Handshake{}, fmt.Errorf("invalid character %q in protocol %q", c, v)
				}
			}
		}
	}
	req.Header.Set("Sec-WebSocket-Protocol",
		strings.Join(opts.SupportedProtocols, ", "),
	)
	req.Header.Del("Sec-Websocket-Extensions")

	// add "context" to request
	req = req.WithContext(ctx)

	// send request
	resp, err := d.HTTPClient.Do(req)
	if err != nil {
		return nil, Handshake{}, err
	}
	if resp.StatusCode != http.StatusSwitchingProtocols {
		defer resp.Body.Close()
		if resp.StatusCode == 400 && resp.Header["Sec-Websocket-Version"] != nil {
			return nil, Handshake{
					Method:    http.MethodGet,
					HTTPMajor: resp.ProtoMajor,
					HTTPMinor: resp.ProtoMinor,
				}, fmt.Errorf("client supports version 13 (server supports: %s)",
					strings.Join(resp.Header["Sec-Websocket-Version"], ", "),
				)
		}
		if resp.StatusCode == http.StatusMethodNotAllowed {
			return nil, Handshake{
				Method:    http.MethodGet,
				HTTPMajor: resp.ProtoMajor,
				HTTPMinor: resp.ProtoMinor,
			}, errMethodNotAllowed
		}
		if resp.StatusCode >= 400 {
			return nil, Handshake{
					Method:    http.MethodGet,
					HTTPMajor: resp.ProtoMajor,
					HTTPMinor: resp.ProtoMinor,
				}, fmt.Errorf("got http error code %d (%s)",
					resp.StatusCode,
					http.StatusText(resp.StatusCode),
				)
		}
		return nil, Handshake{
				Method:    http.MethodGet,
				HTTPMajor: resp.ProtoMajor,
				HTTPMinor: resp.ProtoMinor,
			}, fmt.Errorf("expected http status 101 (switching protocols) but got http status %d (%s)",
				resp.StatusCode,
				http.StatusText(resp.StatusCode),
			)
	}

	// validate response
	switch {
	case !strings.EqualFold(resp.Header.Get("Upgrade"), "websocket"):
		defer resp.Body.Close()
		return nil, Handshake{
			Method:    http.MethodGet,
			HTTPMajor: resp.ProtoMajor,
			HTTPMinor: resp.ProtoMinor,
		}, errors.New("bad response upgrade field")
	case !strings.EqualFold(resp.Header.Get("Connection"), "upgrade"):
		defer resp.Body.Close()
		return nil, Handshake{
			Method:    http.MethodGet,
			HTTPMajor: resp.ProtoMajor,
			HTTPMinor: resp.ProtoMinor,
		}, errors.New("bad response connection field")
	case !strings.EqualFold(resp.Header.Get("Sec-WebSocket-Version"), "13"):
		defer resp.Body.Close()
		return nil, Handshake{
			Method:    http.MethodGet,
			HTTPMajor: resp.ProtoMajor,
			HTTPMinor: resp.ProtoMinor,
		}, errors.New("unsupported websocket version")
	case !strings.EqualFold(resp.Header.Get("Sec-WebSocket-Accept"), challengeResponse(req)):
		defer resp.Body.Close()
		return nil, Handshake{
			Method:    http.MethodGet,
			HTTPMajor: resp.ProtoMajor,
			HTTPMinor: resp.ProtoMinor,
		}, errors.New("bad challenge response")
	}

	// validate protocol negotiation
	if len(opts.SupportedProtocols) > 0 && resp.Header["Sec-Websocket-Protocol"] != nil {
		proto := resp.Header.Get("Sec-Websocket-Protocol")
		var confirmed bool
		for _, v := range opts.SupportedProtocols {
			if v == proto {
				confirmed = true
				break
			}
		}
		if !confirmed {
			defer resp.Body.Close()
			return nil, Handshake{
					Method:    http.MethodGet,
					HTTPMajor: resp.ProtoMajor,
					HTTPMinor: resp.ProtoMinor,
				}, fmt.Errorf("unsupported websocket protocol %q (supported: %s)",
					proto,
					strings.Join(opts.SupportedProtocols, ", "),
				)
		}
	}

	// set up I/O
	w, ok := resp.Body.(io.Writer)
	if !ok {
		defer resp.Body.Close()
		return nil, Handshake{
			Method:    http.MethodGet,
			HTTPMajor: resp.ProtoMajor,
			HTTPMinor: resp.ProtoMinor,
		}, errors.New("response not writeable")
	}
	return &Conn{
			brw: &bufio.ReadWriter{
				Reader: bufio.NewReader(resp.Body),
				Writer: bufio.NewWriter(w),
			},
			close:  resp.Body,
			closed: make(chan struct{}),
		}, Handshake{
			Method:    http.MethodGet,
			HTTPMajor: resp.ProtoMajor,
			HTTPMinor: resp.ProtoMinor,
			Protocol:  resp.Header.Get("Sec-Websocket-Protocol"),
			Version:   13,
		}, nil
}

var errMethodNotAllowed = errors.New("method not allowed")

func (d *Dialer) dialHTTP2(ctx context.Context, u *url.URL, opts HandshakeOptions) (*Conn, Handshake, error) {
	// create request
	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, Handshake{}, err
	}

	// generate headers
	if len(opts.Headers) > 0 {
		req.Header = opts.Headers
	}
	ch, err := d.challenge()
	if err != nil {
		return nil, Handshake{}, err
	}
	req.Header.Set(":protocol", "websocket")
	// ":authority"????
	req.Header.Set("Sec-WebSocket-Key", ch)
	req.Header.Set("Sec-WebSocket-Version", "13")
	if len(opts.SupportedProtocols) > 0 {
		for _, v := range opts.SupportedProtocols {
			for _, c := range []rune(v) {
				switch {
				case c >= 'a' && c <= 'z':
				case c >= 'A' && c <= 'Z':
				default:
					return nil, Handshake{}, fmt.Errorf("invalid character %q in protocol %q", c, v)
				}
			}
		}
	}
	req.Header.Set("Sec-WebSocket-Protocol",
		strings.Join(opts.SupportedProtocols, ", "),
	)
	req.Header.Del("Sec-Websocket-Extensions")

	// add "context" to request
	req = req.WithContext(ctx)

	// send request
	resp, err := d.HTTPClient.Do(req)
	if err != nil {
		return nil, Handshake{}, err
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		if resp.StatusCode == 400 && resp.Header["Sec-Websocket-Version"] != nil {
			return nil, Handshake{
					Method:    http.MethodGet,
					HTTPMajor: resp.ProtoMajor,
					HTTPMinor: resp.ProtoMinor,
				}, fmt.Errorf("client supports version 13 (server supports: %s)",
					strings.Join(resp.Header["Sec-Websocket-Version"], ", "),
				)
		}
		if resp.StatusCode == http.StatusMethodNotAllowed {
			return nil, Handshake{
				Method:    http.MethodGet,
				HTTPMajor: resp.ProtoMajor,
				HTTPMinor: resp.ProtoMinor,
			}, errMethodNotAllowed
		}
		if resp.StatusCode >= 400 {
			return nil, Handshake{
					Method:    http.MethodGet,
					HTTPMajor: resp.ProtoMajor,
					HTTPMinor: resp.ProtoMinor,
				}, fmt.Errorf("got http error code %d (%s)",
					resp.StatusCode,
					http.StatusText(resp.StatusCode),
				)
		}
		return nil, Handshake{
				Method:    http.MethodGet,
				HTTPMajor: resp.ProtoMajor,
				HTTPMinor: resp.ProtoMinor,
			}, fmt.Errorf("expected http status 200 (OK) but got http status %d (%s)",
				resp.StatusCode,
				http.StatusText(resp.StatusCode),
			)
	}

	// validate response
	switch {
	case !strings.EqualFold(resp.Header.Get("Sec-WebSocket-Version"), "13"):
		defer resp.Body.Close()
		return nil, Handshake{
			Method:    http.MethodGet,
			HTTPMajor: resp.ProtoMajor,
			HTTPMinor: resp.ProtoMinor,
		}, errors.New("unsupported websocket version")
	case !strings.EqualFold(resp.Header.Get("Sec-WebSocket-Accept"), challengeResponse(req)):
		defer resp.Body.Close()
		return nil, Handshake{
			Method:    http.MethodGet,
			HTTPMajor: resp.ProtoMajor,
			HTTPMinor: resp.ProtoMinor,
		}, errors.New("bad challenge response")
	}

	// validate protocol negotiation
	if len(opts.SupportedProtocols) > 0 && resp.Header["Sec-Websocket-Protocol"] != nil {
		proto := resp.Header.Get("Sec-Websocket-Protocol")
		var confirmed bool
		for _, v := range opts.SupportedProtocols {
			if v == proto {
				confirmed = true
				break
			}
		}
		if !confirmed {
			defer resp.Body.Close()
			return nil, Handshake{
					Method:    http.MethodGet,
					HTTPMajor: resp.ProtoMajor,
					HTTPMinor: resp.ProtoMinor,
				}, fmt.Errorf("unsupported websocket protocol %q (supported: %s)",
					proto,
					strings.Join(opts.SupportedProtocols, ", "),
				)
		}
	}

	// set up I/O
	w, ok := resp.Body.(io.Writer)
	if !ok {
		defer resp.Body.Close()
		return nil, Handshake{
			Method:    http.MethodGet,
			HTTPMajor: resp.ProtoMajor,
			HTTPMinor: resp.ProtoMinor,
		}, errors.New("response not writeable")
	}
	return &Conn{
			brw: &bufio.ReadWriter{
				Reader: bufio.NewReader(resp.Body),
				Writer: bufio.NewWriter(w),
			},
			close:  resp.Body,
			closed: make(chan struct{}),
		}, Handshake{
			Method:    http.MethodGet,
			HTTPMajor: resp.ProtoMajor,
			HTTPMinor: resp.ProtoMinor,
			Protocol:  resp.Header.Get("Sec-Websocket-Protocol"),
			Version:   13,
		}, nil
}

// Dial creates a websocket connection.
func (d *Dialer) Dial(ctx context.Context, u *url.URL, opts HandshakeOptions) (*Conn, Handshake, error) {
	// code temporarily commented out because http/2 support is broken
	/*switch {
	case d.DisableHTTP1 && d.DisableHTTP2:
		return nil, Handshake{}, errors.New("both HTTP/1 and HTTP/2 are disabled")
	case d.DisableHTTP2:*/
	c, h, err := d.dialHTTP1(ctx, u, opts)
	if err != nil {
		return nil, h, err
	}
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		c.pingLoop(opts.PingInterval, opts.PongTimeout)
	}()
	return c, h, nil
	/*case d.PreferHTTP1:
		c, h, err := d.dialHTTP1(ctx, u, opts)
		if err != nil {
			// upgrade to HTTP/2
			if err == errMethodNotAllowed && !d.DisableHTTP2 {
				return d.dialHTTP2(ctx, u, opts)
			}
			return nil, h, err
		}

		return c, h, nil
	default:
		c, h, err := d.dialHTTP2(ctx, u, opts)
		if err != nil {
			// downgrade to HTTP/1
			if err == errMethodNotAllowed && !d.DisableHTTP1 {
				return d.dialHTTP1(ctx, u, opts)
			}
			return nil, h, err
		}
		return c, h, nil
	}*/
}

// Upgrade handles an incoming websocket handshake.
func Upgrade(w http.ResponseWriter, r *http.Request, opts HandshakeOptions) (*Conn, Handshake, error) {
	switch r.Method {
	case http.MethodGet:
		// ensure conformant http version
		if !r.ProtoAtLeast(1, 1) {
			return nil, Handshake{
				Method: http.MethodGet,
			}, errors.New("unsupported HTTP version")
		}

		// check special headers
		switch {
		case !strings.EqualFold(r.Header.Get("Upgrade"), "websocket"):
			http.Error(w, "bad request upgrade field", http.StatusBadRequest)
			return nil, Handshake{
				Method:    http.MethodGet,
				HTTPMajor: r.ProtoMajor,
				HTTPMinor: r.ProtoMinor,
			}, errors.New("bad request upgrade field")
		case !strings.EqualFold(r.Header.Get("Connection"), "upgrade"):
			http.Error(w, "bad request connection field", http.StatusBadRequest)
			return nil, Handshake{
				Method:    http.MethodGet,
				HTTPMajor: r.ProtoMajor,
				HTTPMinor: r.ProtoMinor,
			}, errors.New("bad request connection field")
		}

		// add special headers to response
		w.Header().Set("Upgrade", "websocket")
		w.Header().Set("Connection", "Upgrade")

		// answer challenge
		w.Header().Set("Sec-WebSocket-Accept", challengeResponse(r))
	/*case http.MethodConnect:
	if !strings.EqualFold(":protocol", "websocket") {
		http.Error(w, "protocol is not websocket", http.StatusBadRequest)
		return nil, Handshake{
			Method:    http.MethodGet,
			HTTPMajor: r.ProtoMajor,
			HTTPMinor: r.ProtoMinor,
		}, errors.New("protocol is not websocket")
	}*/
	default:
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return nil, Handshake{
			Method:    http.MethodGet,
			HTTPMajor: r.ProtoMajor,
			HTTPMinor: r.ProtoMinor,
		}, errors.New("method not allowed")
	}

	// protocol negotiation
	if len(opts.SupportedProtocols) > 0 {
		var proto string
	match:
		for _, v := range r.Header["Sec-Websocket-Protocol"] {
			for _, a := range strings.Split(v, ", ") {
				for _, b := range opts.SupportedProtocols {
					if a == b {
						proto = a
						break match
					}
				}
			}
		}
		w.Header().Set("Sec-WebSocket-Protocol", proto)
	}

	w.Header().Set("Sec-WebSocket-Version", "13")

	// send status code
	switch r.Method {
	case http.MethodGet:
		w.WriteHeader(http.StatusSwitchingProtocols)
	case http.MethodConnect:
		w.WriteHeader(http.StatusOK)
	}

	// hijack connection
	h, ok := w.(http.Hijacker)
	if !ok {
		return nil, Handshake{
			Method:    http.MethodGet,
			HTTPMajor: r.ProtoMajor,
			HTTPMinor: r.ProtoMinor,
			Version:   13,
			Protocol:  w.Header().Get("Sec-WebSocket-Protocol"),
		}, errors.New("connection not hijackable")
	}
	c, brw, err := h.Hijack()
	if err != nil {
		return nil, Handshake{
			Method:    http.MethodGet,
			HTTPMajor: r.ProtoMajor,
			HTTPMinor: r.ProtoMinor,
			Version:   13,
			Protocol:  w.Header().Get("Sec-WebSocket-Protocol"),
		}, errors.New("failed to hijack connection")
	}

	// finish
	wsc := &Conn{
		conn:   c,
		brw:    brw,
		close:  c,
		closed: make(chan struct{}),
	}
	wsc.wg.Add(1)
	go func() {
		defer wsc.wg.Done()
		wsc.pingLoop(opts.PingInterval, opts.PongTimeout)
	}()
	return wsc, Handshake{
		Method:    http.MethodGet,
		HTTPMajor: r.ProtoMajor,
		HTTPMinor: r.ProtoMinor,
		Version:   13,
		Protocol:  w.Header().Get("Sec-WebSocket-Protocol"),
	}, nil
}
