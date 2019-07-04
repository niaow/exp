// Package ws implements WebSockets, as defined in RFC 6455 and RFC 8441.
// It can automatically respond to pings.
// It also has (WIP) support for HTTP/2.
// Most notably, it only uses a standard *http.Client from "net/http".
// See examples/chat for a working example of using this package.
//
//
// References:
// 	RFC 6455 - https://tools.ietf.org/html/rfc6455
// 	RFC 8441 - https://tools.ietf.org/html/rfc8441
package ws

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// header is a websocket frame header
// https://tools.ietf.org/html/rfc6455#section-5.2
type header struct {
	fin              bool
	rsv1, rsv2, rsv3 bool
	opcode           uint8
	mask             bool
	length           uint64
	maskKey          [4]byte
}

// standard frame header opcodes
// https://tools.ietf.org/html/rfc6455#section-5.2
const (
	opContinue uint8 = 0
	opText     uint8 = 1
	opBinary   uint8 = 2
	opClose    uint8 = 8
	opPing     uint8 = 9
	opPong     uint8 = 10
)

// readHeader reads a frame header
func readHeader(r io.Reader) (header, error) {
	buf := make([]byte, 16/8, 64/8)
	_, err := io.ReadFull(r, buf)
	if err != nil {
		return header{}, err
	}
	f := header{
		fin:    (buf[0] & (1 << 7)) != 0,
		rsv1:   (buf[0] & (1 << 6)) != 0,
		rsv2:   (buf[0] & (1 << 5)) != 0,
		rsv3:   (buf[0] & (1 << 4)) != 0,
		opcode: buf[0] & ((1 << 4) - 1),
		mask:   (buf[1] & (1 << 7)) != 0,
	}
	l := buf[1] & ((1 << 7) - 1)
	switch l {
	default:
		f.length = uint64(l)
	case 126:
		buf = buf[:16/8]
		_, err := io.ReadFull(r, buf)
		if err != nil {
			return header{}, err
		}
		f.length = uint64(binary.BigEndian.Uint16(buf))
	case 127:
		buf = buf[:64/8]
		_, err := io.ReadFull(r, buf)
		if err != nil {
			return header{}, err
		}
		f.length = uint64(binary.BigEndian.Uint64(buf))
	}
	if f.mask {
		_, err := io.ReadFull(r, f.maskKey[:])
		if err != nil {
			return header{}, err
		}
	}
	return f, nil
}

// boolToByte returns the bit corresponding to the given bool
func boolToByte(v bool) byte {
	if v {
		return 1
	} else {
		return 0
	}
}

// write writes the given header to the writer without flushing
func (h header) write(w *bufio.Writer) error {
	err := w.WriteByte(
		boolToByte(h.fin)<<7 |
			boolToByte(h.rsv1)<<6 |
			boolToByte(h.rsv2)<<5 |
			boolToByte(h.rsv3)<<4 |
			h.opcode,
	)
	if err != nil {
		return err
	}
	var l byte
	switch {
	case h.length <= 125:
		l = byte(h.length)
	case h.length <= (1<<16)-1:
		l = 126
	default:
		l = 127
	}
	err = w.WriteByte(boolToByte(h.mask)<<7 | l)
	if err != nil {
		return err
	}
	switch l {
	case 126:
		buf := make([]byte, 16/8)
		binary.BigEndian.PutUint16(buf, uint16(h.length))
		_, err = w.Write(buf)
		if err != nil {
			return err
		}
	case 127:
		buf := make([]byte, 64/8)
		binary.BigEndian.PutUint64(buf, h.length)
		_, err = w.Write(buf)
		if err != nil {
			return err
		}
	}
	if h.mask {
		_, err = w.Write(h.maskKey[:])
		if err != nil {
			return err
		}
	}
	return nil
}

// cad is a concurrent access detector
type cad uint32

func (c *cad) acquire(name string) {
	if !atomic.CompareAndSwapUint32((*uint32)(c), 0, 1) {
		panic(fmt.Errorf("concurrent %s access", name))
	}
}

func (c *cad) release(name string) {
	if !atomic.CompareAndSwapUint32((*uint32)(c), 1, 0) {
		panic(fmt.Errorf("double release of %s cad", name))
	}
}

// Conn is a websocket connection.
// At most one concurrent writer is permitted (including graceful closures).
// At most one concurrent reader is permitted.
// Forced closures can be done at any time.
// Pings will only be responded to during calls to NextFrame.
type Conn struct {
	// conn is the underlying connection, if present
	conn net.Conn

	// brw is the buffered input/output for the connection
	brw *bufio.ReadWriter

	// close is the interface used to close the underlying connection
	close io.Closer

	// writeLock is locked when starting a frame and unlocked after
	writeLock sync.Mutex

	// writeLength is the remaining length of the frame write
	writeLength uint64

	// streamWrite says whether the write end is in stream mode
	// in stream mode, each write is sent as a fragmented frame
	streamWrite bool

	// readLength is the remaining length of the frame being read
	readLength uint64

	// readFrame is the header of the frame being currently read
	readFrame header

	// concurrent access detection
	writeCAD, controlCAD, readCAD cad

	// closed is a channel to be used to notify of closure
	closed chan struct{}

	notFirstRead bool

	// ping-pong
	wg       sync.WaitGroup
	lastPong uint32

	closeSent   bool
	closeReason error

	je      *json.Encoder
	jeAlloc sync.Once
}

// ErrAlreadyClosed is an error indicating that the operation failed because the connection was closed.
var ErrAlreadyClosed = errors.New("write after WebSocket connection already closed")

func (c *Conn) pingLoop(interval time.Duration, timeout time.Duration) {
	if interval == 0 {
		interval = 30 * time.Second
	}
	if timeout == 0 {
		timeout = 2 * interval
	}

	nTimeout := timeout / interval
	if timeout%interval != 0 {
		nTimeout++
	}
	tick := time.NewTicker(interval)
	defer tick.Stop()
	var lastPing uint32
	strikesRemaining := nTimeout
	for {
		select {
		case <-c.closed:
			return
		case <-tick.C:
			if atomic.LoadUint32(&c.lastPong) < lastPing {
				strikesRemaining--
				if strikesRemaining == 0 {
					c.forceClose()
					return
				}
			} else {
				strikesRemaining = nTimeout
				lastPing++
				err := c.ping([]byte(strconv.FormatUint(uint64(lastPing), 10)))
				if err != nil {
					c.forceClose()
					return
				}
			}
		}
	}
}

func tryClose(ch chan struct{}) {
	defer func() { recover() }()
	close(ch)
}

func (c *Conn) startFrame(h header) (err error) {
	defer func() {
		if err != nil {
			select {
			case <-c.closed:
				err = ErrAlreadyClosed
			default:
			}
		}
	}()
	c.writeLock.Lock()
	err = h.write(c.brw.Writer)
	if err != nil {
		c.writeLock.Unlock()
		return err
	}
	c.writeLength = h.length
	return nil
}

// StartText starts a text frame of the given length.
func (c *Conn) StartText(length uint64) error {
	c.writeCAD.acquire("write")
	defer c.writeCAD.release("write")

	return c.startFrame(header{
		fin:    true,
		opcode: opText,
		length: length,
	})
}

// StartBinary starts a binary frame of the given length.
func (c *Conn) StartBinary(length uint64) error {
	c.writeCAD.acquire("write")
	defer c.writeCAD.release("write")

	return c.startFrame(header{
		fin:    true,
		opcode: opBinary,
		length: length,
	})
}

// StartTextStream starts a text stream.
func (c *Conn) StartTextStream() error {
	c.writeCAD.acquire("write")
	defer c.writeCAD.release("write")

	err := c.startFrame(header{
		opcode: opText,
	})
	if err != nil {
		return err
	}

	c.streamWrite = true

	return nil
}

// StartBinaryStream starts a binary stream.
func (c *Conn) StartBinaryStream() error {
	c.writeCAD.acquire("write")
	defer c.writeCAD.release("write")

	err := c.startFrame(header{
		opcode: opBinary,
	})
	if err != nil {
		return err
	}

	c.streamWrite = true

	return nil
}

// End ends the current frame or stream.
// This must be called before starting a new frame.
func (c *Conn) End() (err error) {
	c.writeCAD.acquire("write")
	defer c.writeCAD.release("write")

	defer func() {
		if err != nil {
			select {
			case <-c.closed:
				err = ErrAlreadyClosed
			default:
			}
		}
	}()

	if c.streamWrite {
		err = header{
			fin:    true,
			opcode: opContinue,
		}.write(c.brw.Writer)
		if err != nil {
			c.writeLock.Unlock()
			return err
		}
	} else {
		if c.writeLength != 0 {
			c.writeLock.Unlock()
			return errors.New("incomplete frame write")
		}
	}
	err = c.brw.Writer.Flush()
	if err != nil {
		c.writeLock.Unlock()
		return err
	}

	c.writeLock.Unlock()

	return nil
}

// Write writes to the current frame or stream.
func (c *Conn) Write(dat []byte) (n int, err error) {
	c.writeCAD.acquire("write")
	defer c.writeCAD.release("write")

	defer func() {
		if err != nil {
			select {
			case <-c.closed:
				err = ErrAlreadyClosed
			default:
			}
		}
	}()

	if c.streamWrite {
		err = header{
			fin:    false,
			opcode: opContinue,
			length: uint64(len(dat)),
		}.write(c.brw.Writer)
		if err != nil {
			c.writeLock.Unlock()
			return 0, err
		}

		_, err = c.brw.Write(dat)
		if err != nil {
			c.writeLock.Unlock()
			return 0, err
		}
	} else {
		if uint64(len(dat)) <= c.writeLength {
			_, err = c.brw.Write(dat)
			if err != nil {
				c.writeLock.Unlock()
				return 0, err
			}

			c.writeLength -= uint64(len(dat))
		} else {
			c.writeLock.Unlock()
			return 0, errors.New("oversize write")
		}
	}

	return len(dat), nil
}

// SendText sends a text frame with the given string.
func (c *Conn) SendText(txt string) error {
	err := c.StartText(uint64(len(txt)))
	if err != nil {
		return err
	}
	_, err = io.WriteString(c, txt)
	if err != nil {
		return err
	}
	return c.End()
}

// SendBinary sends a binary frame with the given data.
func (c *Conn) SendBinary(dat []byte) error {
	err := c.StartBinary(uint64(len(dat)))
	if err != nil {
		return err
	}
	_, err = c.Write(dat)
	if err != nil {
		return err
	}
	return c.End()
}

// SendJSON sends the given data as JSON in a text frame.
func (c *Conn) SendJSON(v interface{}) error {
	c.jeAlloc.Do(func() {
		c.je = json.NewEncoder(c)
	})
	err := c.StartTextStream() // TODO: send small JSON in a single frame
	if err != nil {
		return err
	}
	err = c.je.Encode(v)
	if err != nil {
		return err
	}
	return c.End()
}

// writeControl writes a control frame
func (c *Conn) writeControl(h header, dat []byte) error {
	c.controlCAD.acquire("control")
	defer c.controlCAD.release("control")

	c.writeLock.Lock()
	defer c.writeLock.Unlock()

	err := h.write(c.brw.Writer)
	if err != nil {
		return err
	}

	_, err = c.brw.Write(dat)
	if err != nil {
		return err
	}

	err = c.brw.Flush()
	if err != nil {
		return err
	}

	return nil
}

const (
	// TextFrame is a frame containing text.
	TextFrame = iota + 1

	// BinaryFrame is a frame containing binary data.
	BinaryFrame
)

func (c *Conn) sendPong(h header) error {
	c.writeLock.Lock()
	defer c.writeLock.Unlock()

	err := header{
		fin:    true,
		opcode: opPong,

		// length is supposed to be less than 125
		// rather than return an error and kill the connection,
		// we tolerate longer ping messages
		// but please, don't send a big ping because it will mess things up
		length: h.length,
	}.write(c.brw.Writer)
	if err != nil {
		return err
	}

	if h.length > (1 << 16) {
		// someone is messing with us
		c.ForceClose()
		return errors.New("gigantic ping packet")
	}

	_, err = io.CopyN(c.brw, c.brw, int64(h.length))
	if err != nil {
		return err
	}

	err = c.brw.Flush()
	if err != nil {
		return err
	}

	return nil
}

var errBadCloseMessage = errors.New("bad close message")

// ErrCloseMessage is an error indicating that the connection was closed by the other side.
type ErrCloseMessage struct {
	rawMsg []byte
}

// Code returns the status code of the closure.
func (err ErrCloseMessage) Code() (uint16, error) {
	if len(err.rawMsg) < 2 {
		return 0, errBadCloseMessage
	}
	return binary.BigEndian.Uint16(err.rawMsg[:2]), nil
}

// Reason returns the reason text for the closure.
func (err ErrCloseMessage) Reason() (string, error) {
	if len(err.rawMsg) < 2 {
		return "", errBadCloseMessage
	}
	return string(err.rawMsg[2:]), nil
}

func (err ErrCloseMessage) Error() string {
	code, derr := err.Code()
	if derr != nil {
		return "bad close message"
	}
	reason, derr := err.Reason()
	if derr != nil {
		return "bad close message"
	}

	if reason == "" {
		return fmt.Sprintf("closed with code %d", code)
	}
	return fmt.Sprintf("closed with code %d: %q", code, reason)
}

func (c *Conn) respClose(h header) error {
	c.writeLock.Lock()
	defer c.writeLock.Unlock()

	if !c.closeSent {
		err := header{
			fin:    true,
			opcode: opClose,

			// length is supposed to be less than 125
			length: h.length,
		}.write(c.brw.Writer)
		if err != nil {
			return err
		}
	}

	if h.length > 125 {
		c.ForceClose()
		return errors.New("oversized close frame")
	}

	var cmsg []byte
	if c.closeSent {
		_, err := io.CopyN(ioutil.Discard, c.brw, int64(h.length))
		if err != nil {
			return err
		}
	} else {
		var buf bytes.Buffer
		_, err := io.CopyN(c.brw, io.TeeReader(c.brw, &buf), int64(h.length))
		if err != nil {
			return err
		}
		cmsg = buf.Bytes()
	}

	err := c.brw.Flush()
	if err != nil {
		return err
	}

	if !c.closeSent {
		c.closeReason = ErrCloseMessage{cmsg}
	}

	return nil
}

// ErrClosed is an error returned when a close frame is recieved.
type ErrClosed struct {
	Err error
}

func (err ErrClosed) Error() string {
	return fmt.Sprintf("closed: %s", err.Err.Error())
}

// NextFrame reads the header of the next frame and returns an the frame type.
// If a ping is encountered, it will be responded to, then another frame will be read.
// The error io.EOF will be returned when a response to a close frame is recieved.
// An error of the type ErrClosed will be returned when the opposite side closes the connection.
func (c *Conn) NextFrame() (int, error) {
	c.readCAD.acquire("read")
	defer c.readCAD.release("read")

	if c.readLength > 0 || (!c.readFrame.fin && c.notFirstRead) {
		return 0, errors.New("previous frame not fully read")
	}

frame:
	h, err := readHeader(c.brw)
	if err != nil {
		return 0, err
	}
	switch h.opcode {
	case opText:
		c.readLength, c.readFrame = h.length, h
		c.notFirstRead = true
		return TextFrame, nil
	case opBinary:
		c.readLength, c.readFrame = h.length, h
		c.notFirstRead = true
		return BinaryFrame, nil
	case opPong:
		if h.length > 125 {
			return 0, errors.New("oversized pong frame")
		}
		buf := make([]byte, h.length)
		_, err = io.ReadFull(c.brw, buf)
		if err != nil {
			return 0, fmt.Errorf("failed to read pong: %s", err)
		}
		n, err := strconv.ParseUint(string(buf), 10, 32)
		if err != nil {
			return 0, fmt.Errorf("failed to read pong: %s", err)
		}
		if !atomic.CompareAndSwapUint32(&c.lastPong, uint32(n)-1, uint32(n)) {
			return 0, fmt.Errorf("failed to process pong: incorrect payload (expected %d but got %d)", atomic.LoadUint32(&c.lastPong)+1, n)
		}
		goto frame
	case opContinue:
		return 0, errors.New("found a continue frame without a starting frame")
	case opPing:
		err = c.sendPong(h)
		if err != nil {
			return 0, err
		}
		goto frame
	case opClose:
		err := c.respClose(h)
		if err != nil {
			return 0, err
		}
		c.ForceClose()
		if c.closeReason != nil {
			return 0, ErrClosed{c.closeReason}
		}
		return 0, io.EOF
	default:
		return 0, fmt.Errorf("unrecognized frame opcode %d", h.opcode)
	}
}

// Read reads from the current frame.
// It will automatically move onto continuation frames.
// When the full frame ends, it will return io.EOF.
func (c *Conn) Read(buf []byte) (int, error) {
	c.readCAD.acquire("read")
	defer c.readCAD.release("read")

start:
	switch {
	case c.readLength == 0 && c.readFrame.fin:
		return 0, io.EOF
	case c.readLength == 0:
		h, err := readHeader(c.brw)
		if err != nil {
			return 0, err
		}
		if h.opcode != opContinue {
			return 0, fmt.Errorf("expected continuation frame but got opcode %d", h.opcode)
		}
		c.readLength, c.readFrame = h.length, h
		goto start
	case uint64(len(buf)) > c.readLength:
		buf = buf[:c.readLength]
		fallthrough
	default:
		_, err := c.brw.Read(buf)
		if err != nil {
			return 0, err
		}
		if c.readFrame.mask {
			for i, v := range buf {
				buf[i] = v ^ c.readFrame.maskKey[i%4]
			}
		}
		c.readLength -= uint64(len(buf))
		return len(buf), nil
	}
}

// ping sends a ping message over the connection.
// ping may be called concurrently with writers.
// However, ping may not be called concurrently with itself.
func (c *Conn) ping(dat []byte) error {
	if len(dat) > 125 {
		return errors.New("ping exceeds max length")
	}

	return c.writeControl(header{
		fin:    true,
		opcode: opPing,
		length: uint64(len(dat)),
	}, dat)
}

// ReadJSON reads the current frame as JSON and stores it into the given value.
func (c *Conn) ReadJSON(v interface{}) error {
	dat, err := ioutil.ReadAll(c)
	if err != nil {
		return err
	}
	return json.Unmarshal(dat, v)
}

// writeClose writes a closure frame
func (c *Conn) writeClose(code uint16, reason string) error {
	c.writeLock.Lock()
	defer c.writeLock.Unlock()

	if len(reason)+2 > 125 {
		reason = reason[:125-5] + "..."
	}
	err := header{
		fin:    true,
		opcode: opClose,
		length: uint64(len(reason)) + 2,
	}.write(c.brw.Writer)
	if err != nil {
		return err
	}
	buf := make([]byte, 2)
	binary.BigEndian.PutUint16(buf, code)
	_, err = c.brw.Write(buf)
	if err != nil {
		return err
	}
	_, err = c.brw.WriteString(reason)
	if err != nil {
		return err
	}
	err = c.brw.Flush()
	if err != nil {
		return err
	}

	return nil
}

// Close attempts to gracefully close the WebSocket connection.
// The reason string must be no more than 123 characters.
// If the context is cancelled, the connection will be immediately terminated.
// It is suggested that a reasonable timeout is applied to the context.
func (c *Conn) Close(ctx context.Context, code uint16, reason string) (err error) {
	c.writeCAD.acquire("write")
	defer c.writeCAD.release("write")

	octx := ctx
	var fcerr error
	defer func() {
		if err != nil {
			select {
			case <-octx.Done():
				err = octx.Err()
			default:
			}
		} else {
			if fcerr != nil {
				err = fcerr
			}
		}
	}()

	// set up timeout/cancellation
	ctx, cancel := context.WithCancel(ctx)
	var wg sync.WaitGroup
	defer wg.Wait()
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-ctx.Done()
		fcerr = c.ForceClose()
	}()
	defer cancel()

	// send closure message
	if err := c.writeClose(code, reason); err != nil {
		return err
	}

	// wait for response
	select {
	case <-c.closed:
	case <-ctx.Done():
		return ctx.Err()
	}

	return nil
}

// forceClose terminates the connection immediately and unsafely, without waiting for ping goroutine shutdown.
func (c *Conn) forceClose() error {
	tryClose(c.closed)
	return c.close.Close()
}

// ForceClose terminates the connection immediately and unsafely.
func (c *Conn) ForceClose() error {
	defer c.wg.Wait()
	return c.forceClose()
}
