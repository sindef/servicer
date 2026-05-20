package adapters

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

type natsStreamSpec struct {
	Name        string   `json:"name,omitempty"`
	Description string   `json:"description,omitempty"`
	Subjects    []string `json:"subjects,omitempty"`
	Storage     string   `json:"storage,omitempty"`
	Retention   string   `json:"retention,omitempty"`
	MaxAge      string   `json:"maxAge,omitempty"`
	MaxMsgs     int64    `json:"maxMsgs,omitempty"`
	MaxBytes    int64    `json:"maxBytes,omitempty"`
	Replicas    *int32   `json:"replicas,omitempty"`
}

type natsConsumerSpec struct {
	Name           string   `json:"name,omitempty"`
	Stream         string   `json:"stream,omitempty"`
	Description    string   `json:"description,omitempty"`
	FilterSubjects []string `json:"filterSubjects,omitempty"`
	AckPolicy      string   `json:"ackPolicy,omitempty"`
	DeliverPolicy  string   `json:"deliverPolicy,omitempty"`
	ReplayPolicy   string   `json:"replayPolicy,omitempty"`
	MaxAckPending  int64    `json:"maxAckPending,omitempty"`
}

type natsPermissionSpec struct {
	Publish        []string `json:"publish,omitempty"`
	Subscribe      []string `json:"subscribe,omitempty"`
	AllowResponses bool     `json:"allowResponses,omitempty"`
}

type natsAppCredentialSpec struct {
	Name        string             `json:"name,omitempty"`
	Username    string             `json:"username,omitempty"`
	Description string             `json:"description,omitempty"`
	Permissions natsPermissionSpec `json:"permissions,omitempty"`
}

func (s natsStreamSpec) normalized() natsStreamSpec {
	s.Name = strings.TrimSpace(s.Name)
	s.Description = firstNonEmpty(strings.TrimSpace(s.Description), fmt.Sprintf("Servicer managed stream %s", s.Name))
	if s.Storage == "" {
		s.Storage = "file"
	}
	if s.Retention == "" {
		s.Retention = "limits"
	}
	if s.MaxAge == "" {
		s.MaxAge = "168h"
	}
	return s
}

func (c natsConsumerSpec) normalized() natsConsumerSpec {
	c.Name = strings.TrimSpace(c.Name)
	c.Stream = strings.TrimSpace(c.Stream)
	c.Description = firstNonEmpty(strings.TrimSpace(c.Description), fmt.Sprintf("Servicer managed consumer %s", c.Name))
	if c.AckPolicy == "" {
		c.AckPolicy = "explicit"
	}
	if c.DeliverPolicy == "" {
		c.DeliverPolicy = "all"
	}
	if c.ReplayPolicy == "" {
		c.ReplayPolicy = "instant"
	}
	if c.MaxAckPending == 0 {
		c.MaxAckPending = 1000
	}
	return c
}

func (c natsAppCredentialSpec) normalized() natsAppCredentialSpec {
	c.Name = strings.TrimSpace(c.Name)
	c.Username = firstNonEmpty(strings.TrimSpace(c.Username), c.Name)
	c.Description = firstNonEmpty(strings.TrimSpace(c.Description), fmt.Sprintf("Servicer managed credential %s", c.Name))
	return c
}

func natsResourceDigest(parameters natsParameters) string {
	payload := map[string]any{
		"credentials": parameters.AppCredentials,
		"consumers":   parameters.Consumers,
		"jetstream":   parameters.JetStream,
		"streams":     parameters.Streams,
	}
	raw, _ := json.Marshal(payload)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])[:10]
}
