package control

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/abhisek343/cutline/internal/model"
)

type ClientConfig struct {
	Network   string
	Address   string
	Token     string
	RunID     model.RunID
	AttemptID model.AttemptID
	SessionID model.SessionID
}

// Client multiplexes acknowledged event calls over one Unix connection.
type Client struct {
	config  ClientConfig
	conn    net.Conn
	encoder *json.Encoder
	decoder *json.Decoder

	writeMu sync.Mutex
	nextSeq uint64
	nextID  atomic.Uint64

	pendingMu sync.Mutex
	pending   map[string]chan callResult

	closeOnce sync.Once
	closed    chan struct{}
	readErr   error
}

type callResult struct {
	response Response
	err      error
}

func Dial(ctx context.Context, config ClientConfig) (*Client, error) {
	if config.Network == "" {
		config.Network = "unix"
	}
	if (config.Network != "unix" && config.Network != "tcp") || config.Address == "" || config.Token == "" {
		return nil, fmt.Errorf("%w: valid network, address, and token are required", ErrProtocol)
	}
	dialer := net.Dialer{}
	conn, err := dialer.DialContext(ctx, config.Network, config.Address)
	if err != nil {
		return nil, fmt.Errorf("dial cutline control endpoint: %w", err)
	}

	client := &Client{
		config:  config,
		conn:    conn,
		encoder: json.NewEncoder(conn),
		decoder: json.NewDecoder(conn),
		pending: make(map[string]chan callResult),
		closed:  make(chan struct{}),
	}
	if err := client.handshake(ctx); err != nil {
		_ = conn.Close()
		return nil, err
	}
	go client.readLoop()
	return client, nil
}

func (c *Client) handshake(ctx context.Context) error {
	request := Request{
		ProtocolVersion: ProtocolVersion,
		RequestID:       "hello",
		Sequence:        1,
		Kind:            RequestHello,
		Token:           c.config.Token,
		RunID:           c.config.RunID,
		AttemptID:       c.config.AttemptID,
		SessionID:       c.config.SessionID,
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = c.conn.SetDeadline(deadline)
		defer c.conn.SetDeadline(noDeadline)
	}
	if err := c.encoder.Encode(request); err != nil {
		return fmt.Errorf("send control hello: %w", err)
	}
	var response Response
	if err := c.decoder.Decode(&response); err != nil {
		return fmt.Errorf("read control hello: %w", err)
	}
	if response.ProtocolVersion != ProtocolVersion || response.ReplyTo != request.RequestID {
		return fmt.Errorf("%w: invalid hello response", ErrProtocol)
	}
	if response.Error != "" {
		return fmt.Errorf("%w: %s", ErrAuthentication, response.Error)
	}
	c.nextSeq = 1
	return nil
}

// SendEvent blocks until the coordinator acknowledges the event and returns any
// checkpoint scheduling decision.
func (c *Client) SendEvent(ctx context.Context, event model.Event) (Decision, error) {
	requestID := fmt.Sprintf("req-%d", c.nextID.Add(1))
	resultCh := make(chan callResult, 1)

	c.pendingMu.Lock()
	select {
	case <-c.closed:
		err := c.readErr
		c.pendingMu.Unlock()
		if err == nil {
			err = ErrDisconnected
		}
		return Decision{}, err
	default:
		c.pending[requestID] = resultCh
	}
	c.pendingMu.Unlock()

	c.writeMu.Lock()
	c.nextSeq++
	event.LocalSequence = c.nextSeq
	request := Request{
		ProtocolVersion: ProtocolVersion,
		RequestID:       requestID,
		Sequence:        c.nextSeq,
		Kind:            RequestEvent,
		RunID:           c.config.RunID,
		AttemptID:       c.config.AttemptID,
		SessionID:       c.config.SessionID,
		Event:           &event,
	}
	err := c.encoder.Encode(request)
	c.writeMu.Unlock()
	if err != nil {
		c.removePending(requestID)
		c.shutdown(fmt.Errorf("%w: write event: %v", ErrDisconnected, err))
		return Decision{}, fmt.Errorf("write control event: %w", err)
	}

	select {
	case result := <-resultCh:
		if result.err != nil {
			return Decision{}, result.err
		}
		if result.response.Error != "" {
			return Decision{}, fmt.Errorf("%w: %s", ErrProtocol, result.response.Error)
		}
		return Decision{
			Action:         result.response.Action,
			CancellationID: result.response.CancellationID,
		}, nil
	case <-ctx.Done():
		c.removePending(requestID)
		return Decision{}, context.Cause(ctx)
	case <-c.closed:
		c.removePending(requestID)
		if c.readErr != nil {
			return Decision{}, c.readErr
		}
		return Decision{}, ErrDisconnected
	}
}

func (c *Client) readLoop() {
	for {
		var response Response
		if err := c.decoder.Decode(&response); err != nil {
			c.shutdown(fmt.Errorf("%w: read response: %v", ErrDisconnected, err))
			return
		}
		if response.ProtocolVersion != ProtocolVersion {
			c.shutdown(fmt.Errorf("%w: response version %d", ErrProtocol, response.ProtocolVersion))
			return
		}
		c.pendingMu.Lock()
		resultCh, ok := c.pending[response.ReplyTo]
		if ok {
			delete(c.pending, response.ReplyTo)
		}
		c.pendingMu.Unlock()
		if ok {
			resultCh <- callResult{response: response}
		}
	}
}

func (c *Client) removePending(requestID string) {
	c.pendingMu.Lock()
	delete(c.pending, requestID)
	c.pendingMu.Unlock()
}

func (c *Client) shutdown(err error) {
	c.closeOnce.Do(func() {
		c.pendingMu.Lock()
		c.readErr = err
		pending := c.pending
		c.pending = make(map[string]chan callResult)
		close(c.closed)
		c.pendingMu.Unlock()
		_ = c.conn.Close()
		for _, resultCh := range pending {
			resultCh <- callResult{err: err}
		}
	})
}

func (c *Client) Close() error {
	c.shutdown(ErrDisconnected)
	return nil
}

func (c *Client) Config() ClientConfig {
	return c.config
}

var noDeadline time.Time
