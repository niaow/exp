package ws_test

import (
	"context"
	"io"
	"io/ioutil"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/jaddr2line/exp/ws"
)

func TestWebSocket(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, h, err := ws.Upgrade(w, r, ws.HandshakeOptions{
			SupportedProtocols: []string{"apple", "orange"},
		})
		if err != nil {
			t.Fatalf("failed handshake on server: %s", err)
		}
		defer c.ForceClose()
		t.Log(h)
		var twg sync.WaitGroup
		defer twg.Wait()
		twg.Add(1)
		go func() {
			defer twg.Done()
			err := c.StartText(5)
			if err != nil {
				t.Fatalf("failed to send hello: %s", err)
			}
			_, err = io.WriteString(c, "hello")
			if err != nil {
				t.Fatalf("failed to send hello: %s", err)
			}
			err = c.End()
			if err != nil {
				t.Fatalf("failed to send hello: %s", err)
			}
		}()
		f, err := c.NextFrame()
		if err != nil {
			t.Fatalf("failed to read frame: %s", err)
		}
		if f != ws.TextFrame {
			t.Fatalf("expected text frame but got %d", f)
		}
		dat, err := ioutil.ReadAll(c)
		if err != nil {
			t.Fatalf("failed to read text: %s", err)
		}
		if string(dat) != "hello" {
			t.Fatalf("expected %q but got %q", "hello", string(dat))
		}
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		err = c.Close(ctx, 1000, "goodbye")
		if err != nil {
			t.Fatalf("failed to close on server side: %s", err)
		}
	}))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute/4)
	defer cancel()
	u, err := url.Parse(srv.URL + "/testws")
	if err != nil {
		t.Fatal(err)
	}
	c, h, err := (&ws.Dialer{
		HTTPClient: srv.Client(),
		Rand:       rand.New(rand.NewSource(5)),
	}).Dial(ctx, u, ws.HandshakeOptions{
		SupportedProtocols: []string{"pear", "apple"},
	})
	defer c.ForceClose()
	if err != nil {
		t.Fatal(err)
	}
	t.Log(h)
	f, err := c.NextFrame()
	if err != nil {
		t.Fatal(err)
	}
	if f != ws.TextFrame {
		t.Fatal(f)
	}
	dat, err := ioutil.ReadAll(c)
	if err != nil {
		t.Fatalf("failed to read text: %s", err)
	}
	if string(dat) != "hello" {
		t.Fatalf("expected %q but got %q", "hello", string(dat))
	}
	err = c.StartTextStream()
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.Write(dat)
	if err != nil {
		t.Fatal(err)
	}
	err = c.End()
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.NextFrame()
	if err == nil {
		t.Fatal("expected closure error, but got none")
	}
	if e, ok := err.(ws.ErrClosed); ok {
		if e, ok := e.Err.(ws.ErrCloseMessage); ok {
			expect := "closed with code 1000: \"goodbye\""
			if e.Error() != expect {
				t.Fatalf("expected %q but got %q", expect, e.Error())
			}
		} else {
			t.Fatalf("expected ErrCloseMessage but got %v", e)
		}
	} else {
		t.Fatalf("expected ErrClosed but got %v", err)
	}
}
