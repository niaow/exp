package main

import (
	"context"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/jaddr2line/exp/ws"
)

func main() {
	sub := make(chan chan<- Message)
	msgch := make(chan Message)
	unsub := make(chan chan<- Message)
	http.HandleFunc("/chat", func(w http.ResponseWriter, r *http.Request) {
		c, h, err := ws.Upgrade(w, r, ws.HandshakeOptions{
			SupportedProtocols: []string{"demo-chat"},
		})
		if err != nil {
			return
		}
		defer c.ForceClose()
		log.Println(h)
		handleConn(c, sub, unsub, msgch)
	})
	go hub(sub, msgch, unsub)
	http.Handle("/", http.FileServer(http.Dir(".")))
	http.ListenAndServe(":9999", nil)
}

func hub(sub <-chan chan<- Message, msgin <-chan Message, unsub <-chan chan<- Message) {
	users := []chan<- Message{}
	for {
		select {
		case u := <-sub:
			users = append(users, u)
		case m := <-msgin:
			log.Println(m)
			for _, u := range users {
				u <- m
			}
		case u := <-unsub:
			for i, v := range users {
				if u == v {
					users[i] = users[len(users)-1]
					users[len(users)-1] = nil
					users = users[:len(users)-1]
					break
				}
			}
		}
	}
}

func handleConn(c *ws.Conn, sub chan<- chan<- Message, unsub chan<- chan<- Message, out chan<- Message) {
	var wg sync.WaitGroup
	defer wg.Wait()

	defer c.ForceClose()

	// get username
	f, err := c.NextFrame()
	if err != nil {
		return
	}
	if f != ws.TextFrame {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		c.Close(ctx, 1003, "expected text data")
		return
	}
	udat, err := ioutil.ReadAll(c)
	if err != nil {
		return
	}
	username := string(udat)

	// start read end
	wg.Add(1)
	done := make(chan struct{})
	go func() {
		defer wg.Done()
		defer close(done)
		defer c.ForceClose()
		for {
			f, err := c.NextFrame()
			if err != nil {
				return
			}
			switch f {
			case ws.TextFrame:
				dat, err := ioutil.ReadAll(c)
				if err != nil {
					return
				}
				out <- Message{
					Sender: username,
					Body:   string(dat),
				}
			default:
				return
			}
		}
	}()

	// subscribe
	mch := make(chan Message)
	sub <- mch
	defer func() {
		select {
		case unsub <- mch:
			// unsubscribe
			return
		case <-mch:
			// ditch message in progress
		}
	}()
	go func() {
		out <- Message{
			Sender: "server",
			Body:   fmt.Sprintf("%q has joined", username),
		}
	}()
	defer func() {
		go func() {
			out <- Message{
				Sender: "server",
				Body:   fmt.Sprintf("%q has left", username),
			}
		}()
	}()

	// start writing side
	for {
		select {
		case <-done:
			return
		case m := <-mch:
			err := c.SendJSON(m)
			if err != nil {
				return
			}
		}
	}
}

// Message is a chat message.
type Message struct {
	Sender string `json:"sender"`
	Body   string `json:"body"`
}
