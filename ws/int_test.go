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

	"github.com/jadr2ddude/exp/ws"
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
			var wwg sync.WaitGroup
			defer wwg.Wait()
			wwg.Add(1)
			go func() {
				defer wwg.Done()
				err := c.Ping([]byte("ping-pong"))
				if err != nil {
					t.Fatalf("failed to send ping: %s", err)
				}
			}()
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
		f, err = c.NextFrame()
		if err != nil {
			t.Fatalf("failed to read frame: %s", err)
		}
		if f != ws.PongFrame {
			t.Fatalf("expected text frame but got %d", f)
		}
		dat, err = ioutil.ReadAll(c)
		if err != nil {
			t.Fatalf("failed to read text: %s", err)
		}
		if string(dat) != "ping-pong" {
			t.Fatalf("expected %q but got %q", "ping-pong", string(dat))
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
		//DisableHTTP2: true,
		Rand: rand.New(rand.NewSource(5)),
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
	if err != io.EOF {
		t.Fatal(err)
	}
}
