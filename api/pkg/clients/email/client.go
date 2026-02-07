package email

import (
	"context"
	"log/slog"
)

// Message represents an email to be sent.
type Message struct {
	To      string
	From    string
	Subject string
	Body    string
}

// Result holds the outcome of a send attempt.
type Result struct {
	DeliveryStatus string
	Sent           bool
}

// Client defines the interface for sending emails.
// Implementations can be swapped between a stub (for dev/testing)
// and a real provider (e.g. SendGrid, SES).
type Client interface {
	Send(ctx context.Context, msg Message) (*Result, error)
}

// StubClient simulates sending emails by logging them.
// Used for development and the coding challenge.
type StubClient struct {
	FromAddress string
}

// NewStubClient creates an email client that logs instead of sending.
func NewStubClient(fromAddress string) *StubClient {
	return &StubClient{FromAddress: fromAddress}
}

func (c *StubClient) Send(_ context.Context, msg Message) (*Result, error) {
	slog.Info("sending email (stub)", "to", msg.To, "from", msg.From, "subject", msg.Subject)
	return &Result{
		DeliveryStatus: "sent",
		Sent:           true,
	}, nil
}
