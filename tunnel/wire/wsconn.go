// Package wire holds the small primitives shared by the tunnel agent and the
// tunnel gateway: a net.Conn adapter over a gorilla WebSocket (so yamux can run
// over the WS), the per-tunnel API key format, the agent version gate, and the
// JSON control frames exchanged over the multiplexed session.
package wire

import (
	"errors"
	"io"
	"net"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// WSConn adapts a *websocket.Conn into a net.Conn carrying a byte stream over
// binary WebSocket messages. yamux runs on top of it. yamux drives a single
// reader and a single writer goroutine, which matches gorilla's "one concurrent
// reader, one concurrent writer" constraint, so no extra locking is needed on
// the hot path; the write mutex below only guards against close races.
type WSConn struct {
	ws     *websocket.Conn
	reader io.Reader // current message reader, advanced lazily
	writeM sync.Mutex
}

// NewWSConn wraps an established websocket connection.
func NewWSConn(ws *websocket.Conn) *WSConn { return &WSConn{ws: ws} }

func (c *WSConn) Read(p []byte) (int, error) {
	for {
		if c.reader == nil {
			mt, r, err := c.ws.NextReader()
			if err != nil {
				return 0, err
			}
			if mt != websocket.BinaryMessage {
				// Ignore non-binary frames (e.g. stray text); keep reading.
				continue
			}
			c.reader = r
		}
		n, err := c.reader.Read(p)
		if errors.Is(err, io.EOF) {
			// End of this message; next Read pulls the following frame.
			c.reader = nil
			if n > 0 {
				return n, nil
			}
			continue
		}
		return n, err
	}
}

func (c *WSConn) Write(p []byte) (int, error) {
	c.writeM.Lock()
	defer c.writeM.Unlock()
	if err := c.ws.WriteMessage(websocket.BinaryMessage, p); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (c *WSConn) Close() error { return c.ws.Close() }

func (c *WSConn) LocalAddr() net.Addr  { return c.ws.LocalAddr() }
func (c *WSConn) RemoteAddr() net.Addr { return c.ws.RemoteAddr() }

func (c *WSConn) SetDeadline(t time.Time) error {
	if err := c.ws.SetReadDeadline(t); err != nil {
		return err
	}
	return c.ws.SetWriteDeadline(t)
}

func (c *WSConn) SetReadDeadline(t time.Time) error  { return c.ws.SetReadDeadline(t) }
func (c *WSConn) SetWriteDeadline(t time.Time) error { return c.ws.SetWriteDeadline(t) }

var _ net.Conn = (*WSConn)(nil)
