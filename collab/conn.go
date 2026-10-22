package cmgo

import (
	"context"
	"log"
	"net/http"

	"github.com/coder/websocket"
)

// client is one connection. id and cursor are guarded by Server.mu.
type client struct {
	send   chan []byte
	cancel context.CancelFunc
	author any    // who is behind the connection, from WithAuthor
	id     string // clientID from the latest cursor message
	cursor []byte // latest cursor message, replayed to new clients
}

func (c *client) writeLoop(ctx context.Context, conn *websocket.Conn) {
	for {
		select {
		case <-ctx.Done():
			return
		case data := <-c.send:
			if err := conn.Write(ctx, websocket.MessageText, data); err != nil {
				c.cancel()
				return
			}
		}
	}
}

func (s *Server) readLoop(ctx context.Context, conn *websocket.Conn, c *client) error {
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return err
		}
		if err := s.handle(c, data); err != nil {
			return err
		}
	}
}

// ServeHTTP upgrades the request to a WebSocket and serves one client.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		log.Printf("cmgo: accept: %v", err)
		return
	}
	defer conn.CloseNow()
	conn.SetReadLimit(1 << 20)

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	c := &client{send: make(chan []byte, 64), cancel: cancel, author: s.authorOf(r)}
	s.join(c)
	defer s.leave(c)
	go c.writeLoop(ctx, conn)

	if err := s.readLoop(ctx, conn, c); err != nil {
		// The reason is shown to the user by the frontend.
		conn.Close(websocket.StatusPolicyViolation, err.Error())
	}
}
