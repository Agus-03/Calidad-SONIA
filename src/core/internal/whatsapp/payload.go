package whatsapp

import (
	"strconv"
	"time"
)

// The shapes Meta actually sends, kept to the fields the turn reads: the rest of the envelope stays in payload_json (F20).
type (
	Payload struct {
		Object string  `json:"object"`
		Entry  []Entry `json:"entry"`
	}

	Entry struct {
		ID      string   `json:"id"`
		Changes []Change `json:"changes"`
	}

	Change struct {
		Field string `json:"field"`
		Value Value  `json:"value"`
	}

	Value struct {
		Metadata Metadata      `json:"metadata"`
		Contacts []MetaContact `json:"contacts"`
		Messages []MetaMessage `json:"messages"`
		Statuses []MetaStatus  `json:"statuses"`
	}

	Metadata struct {
		DisplayPhoneNumber string `json:"display_phone_number"`
		PhoneNumberID      string `json:"phone_number_id"`
	}

	MetaContact struct {
		WaID    string `json:"wa_id"`
		Profile struct {
			Name string `json:"name"`
		} `json:"profile"`
	}

	MetaMessage struct {
		From      string `json:"from"`
		ID        string `json:"id"`
		Timestamp string `json:"timestamp"`
		Type      string `json:"type"`
		Text      *struct {
			Body string `json:"body"`
		} `json:"text"`
		Context *struct {
			ID string `json:"id"`
		} `json:"context"`
	}

	MetaStatus struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
)

// SentAt reads Meta's clock, and falls back to ours: a timestamp we cannot parse must not lose the message (C06).
func (m MetaMessage) SentAt(fallback time.Time) time.Time {
	seconds, err := strconv.ParseInt(m.Timestamp, 10, 64)
	if err != nil {
		return fallback
	}
	return time.Unix(seconds, 0).UTC()
}
