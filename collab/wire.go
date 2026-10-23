package collab

import (
	"encoding/json"
	"log"

	"github.com/asano69/cmgo/authority"
)

// updatesMessage is sent by the server: updates starting at index From.
type updatesMessage struct {
	Type    string             `json:"type"`
	From    int                `json:"from"`
	Updates []authority.Update `json:"updates"`
}

func encode(from int, updates []authority.Update) []byte {
	data, err := json.Marshal(updatesMessage{Type: "updates", From: from, Updates: updates})
	if err != nil {
		// Unreachable: change sets only hold numbers and strings.
		log.Printf("cmgo: encode: %v", err)
		return nil
	}
	return data
}

// pushMessage is sent by a client: its synced version plus unconfirmed updates.
type pushMessage struct {
	Version int                `json:"version"`
	Updates []authority.Update `json:"updates"`
}

type envelope struct {
	Type string `json:"type"`
}
