package ws

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"sync"
	"sync/atomic"
)

type header struct {
	fin              bool
	rsv1, rsv2, rsv3 bool
	opcode           uint8
	mask             bool
	length           uint64
	maskKey          [4]byte
}

const (
	opContinue uint8 = 0
	opText     uint8 = 1
	opBinary   uint8 = 2
	opClose    uint8 = 8
	opPing     uint8 = 9
	opPong     uint8 = 10
)

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

func boolToByte(v bool) byte {
	if v {
		return 1
	} else {
		return 0
	}
}

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
type Conn struct {
	// conn is the underlying connection, if present
	conn net.Conn

	// brw is the buffered input/output for the connection
	brw *bufio.ReadWriter

	// close is the interface used to close the underlying connection
	close io.Closer

	// writeLck is locked when starting a frame and unlocked after
	writeLck sync.Mutex

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

	je      *json.Encoder
	jeAlloc sync.Once
}

func tryClose(ch chan struct{}) {
	defer func() { recover() }()
	close(ch)
}

func (c *Conn) startFrame(h header) error {
	c.writeLck.Lock()
	err := h.write(c.brw.Writer)
	if err != nil {
		c.writeLck.Unlock()
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
func (c *Conn) End() error {
	c.writeCAD.acquire("write")
	defer c.writeCAD.release("write")

	if c.streamWrite {
		err := header{
			fin:    true,
			opcode: opContinue,
		}.write(c.brw.Writer)
		if err != nil {
			c.writeLck.Unlock()
			return err
		}
	} else {
		if c.writeLength != 0 {
			c.writeLck.Unlock()
			return errors.New("incomplete frame write")
		}
	}
	err := c.brw.Writer.Flush()
	if err != nil {
		c.writeLck.Unlock()
		return err
	}

	c.writeLck.Unlock()

	return nil
}

// Write writes to the current frame or stream.
func (c *Conn) Write(dat []byte) (int, error) {
	c.writeCAD.acquire("write")
	defer c.writeCAD.release("write")

	if c.streamWrite {
		err := header{
			fin:    false,
			opcode: opContinue,
			length: uint64(len(dat)),
		}.write(c.brw.Writer)
		if err != nil {
			c.writeLck.Unlock()
			return 0, err
		}

		_, err = c.brw.Write(dat)
		if err != nil {
			c.writeLck.Unlock()
			return 0, err
		}
	} else {
		if uint64(len(dat)) <= c.writeLength {
			_, err := c.brw.Write(dat)
			if err != nil {
				c.writeLck.Unlock()
				return 0, err
			}

			c.writeLength -= uint64(len(dat))
		} else {
			c.writeLck.Unlock()
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

	c.writeLck.Lock()
	defer c.writeLck.Unlock()

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

	// PongFrame is a frame containing a pong.
	PongFrame
)

func (c *Conn) sendPong(h header) error {
	c.writeLck.Lock()
	defer c.writeLck.Unlock()

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

// NextFrame reads the header of the next frame and returns an the frame type.
// If a ping is encountered, it will be responded to, then another frame will be read.
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
		c.readLength, c.readFrame = h.length, h
		c.notFirstRead = true
		return PongFrame, nil
	case opContinue:
		return 0, errors.New("found a continue frame without a starting frame")
	case opPing:
		err := c.sendPong(h)
		if err != nil {
			return 0, err
		}
		goto frame
	case opClose:
		// TODO: actually read close message
		io.CopyN(ioutil.Discard, c.brw, int64(h.length))
		tryClose(c.closed)
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

// Ping sends a ping message over the connection.
func (c *Conn) Ping(dat []byte) error {
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

// Close attempts to gracefully close the WebSocket connection.
// The reason string must be no more than 123 characters.
// If the context is cancelled, the connection will be immediately terminated.
// It is suggested that a reasonable timeout is applied to the context.
func (c *Conn) Close(ctx context.Context, code uint16, reason string) error {
	c.writeCAD.acquire("write")
	defer c.writeCAD.release("write")

	// set up timeout/cancellation
	ctx, cancel := context.WithCancel(ctx)
	var wg sync.WaitGroup
	defer wg.Wait()
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-ctx.Done()
		c.ForceClose()
	}()
	defer cancel()

	c.writeLck.Lock()
	defer c.writeLck.Unlock()

	// send closure message
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

	// wait for response
	select {
	case <-c.closed:
	case <-ctx.Done():
	}

	if err = ctx.Err(); err != nil {
		return err
	}
	return nil
}

// ForceClose terminates the connection immediately and unsafely.
func (c *Conn) ForceClose() error {
	tryClose(c.closed)
	return c.close.Close()
}
