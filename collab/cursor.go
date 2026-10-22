package collab

import (
	"encoding/json"
	"fmt"
)

// cursorMessage identifies the sender of a cursor message. The rest of the
// message (name, color, position) is opaque to the server and relayed as is.
type cursorMessage struct {
	ClientID string `json:"clientID"`
}

// leaveMessage tells the other clients to remove a cursor.
type leaveMessage struct {
	Type     string `json:"type"`
	ClientID string `json:"clientID"`
}

// cursor remembers the client's latest cursor and relays it to the others.
func (h *Server) cursor(c *client, data []byte) error {
	var msg cursorMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return fmt.Errorf("invalid cursor message: %w", err)
	}
	if msg.ClientID == "" {
		return fmt.Errorf("cursor message is missing clientID")
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	c.id = msg.ClientID
	c.cursor = data
	for cl := range h.clients {
		if cl != c {
			deliver(cl, data)
		}
	}
	return nil
}

// sendCursors gives a new client the cursors of everyone already connected.
// h.mu must be held.
func (h *Server) sendCursors(c *client) {
	for cl := range h.clients {
		if cl != c && cl.cursor != nil {
			deliver(c, cl.cursor)
		}
	}
}

// announceLeave removes c's cursor from the remaining clients. h.mu must be
// held and c must already be removed from h.clients.
func (h *Server) announceLeave(c *client) {
	if c.id == "" {
		return
	}
	data, err := json.Marshal(leaveMessage{Type: "leave", ClientID: c.id})
	if err != nil {
		// Unreachable: both fields are plain strings.
		return
	}
	for cl := range h.clients {
		deliver(cl, data)
	}
}
