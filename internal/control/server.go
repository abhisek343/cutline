package control

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"sync"

	"github.com/abhisek343/cutline/internal/model"
)

const maxEventPayloadBytes = 64 << 10

type ServerConfig struct {
	Network   string
	Address   string
	Token     string
	RunID     model.RunID
	AttemptID model.AttemptID
	Handler   Handler
}

type Server struct {
	config   ServerConfig
	listener net.Listener
	done     chan struct{}

	mu    sync.Mutex
	conns map[net.Conn]struct{}
	wg    sync.WaitGroup

	closeOnce sync.Once
}

func Listen(config ServerConfig) (*Server, error) {
	if config.Network == "" {
		config.Network = "unix"
	}
	if (config.Network != "unix" && config.Network != "tcp") || config.Address == "" || config.Token == "" || config.Handler == nil {
		return nil, fmt.Errorf("%w: valid network, address, token, and handler are required", ErrProtocol)
	}
	if config.Network == "unix" {
		if err := os.Remove(config.Address); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("remove stale control socket: %w", err)
		}
	}
	listener, err := net.Listen(config.Network, config.Address)
	if err != nil {
		return nil, fmt.Errorf("listen on %s control endpoint: %w", config.Network, err)
	}
	if config.Network == "tcp" && !listener.Addr().(*net.TCPAddr).IP.IsLoopback() {
		_ = listener.Close()
		return nil, fmt.Errorf("%w: TCP control endpoint must be loopback", ErrProtocol)
	}
	if config.Network == "unix" {
		if err := os.Chmod(config.Address, 0o600); err != nil {
			_ = listener.Close()
			return nil, fmt.Errorf("secure control socket: %w", err)
		}
	}
	config.Address = listener.Addr().String()
	if config.Network == "unix" && config.Address == "" {
		_ = listener.Close()
		return nil, fmt.Errorf("%w: listener returned empty address", ErrProtocol)
	}
	server := &Server{
		config:   config,
		listener: listener,
		done:     make(chan struct{}),
		conns:    make(map[net.Conn]struct{}),
	}
	server.wg.Add(1)
	go server.acceptLoop()
	return server, nil
}

func (s *Server) acceptLoop() {
	defer s.wg.Done()
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-s.done:
				return
			default:
				continue
			}
		}
		s.mu.Lock()
		s.conns[conn] = struct{}{}
		s.mu.Unlock()
		s.wg.Add(1)
		go s.serve(conn)
	}
}

func (s *Server) serve(conn net.Conn) {
	defer s.wg.Done()
	defer func() {
		s.mu.Lock()
		delete(s.conns, conn)
		s.mu.Unlock()
		_ = conn.Close()
	}()

	decoder := json.NewDecoder(conn)
	encoder := json.NewEncoder(conn)

	var hello Request
	if err := decoder.Decode(&hello); err != nil {
		return
	}
	if err := s.validateHello(hello); err != nil {
		_ = encoder.Encode(Response{
			ProtocolVersion: ProtocolVersion,
			ReplyTo:         hello.RequestID,
			Error:           err.Error(),
		})
		return
	}
	if err := s.config.Handler.SessionStarted(Hello{
		RunID: hello.RunID, AttemptID: hello.AttemptID, SessionID: hello.SessionID,
	}); err != nil {
		_ = encoder.Encode(Response{
			ProtocolVersion: ProtocolVersion,
			ReplyTo:         hello.RequestID,
			Error:           err.Error(),
		})
		return
	}
	if err := encoder.Encode(Response{ProtocolVersion: ProtocolVersion, ReplyTo: hello.RequestID}); err != nil {
		return
	}

	lastSequence := hello.Sequence
	for {
		var request Request
		if err := decoder.Decode(&request); err != nil {
			return
		}
		response := Response{ProtocolVersion: ProtocolVersion, ReplyTo: request.RequestID}
		if request.Sequence != lastSequence+1 {
			response.Error = fmt.Sprintf("%v: got %d after %d", ErrSequence, request.Sequence, lastSequence)
			_ = encoder.Encode(response)
			return
		}
		lastSequence = request.Sequence
		if err := s.validateEventRequest(request); err != nil {
			response.Error = err.Error()
			_ = encoder.Encode(response)
			return
		}
		decision, err := s.config.Handler.EventAccepted(*request.Event)
		if err != nil {
			response.Error = err.Error()
		} else {
			response.Action = decision.Action
			response.CancellationID = decision.CancellationID
		}
		if err := encoder.Encode(response); err != nil {
			return
		}
	}
}

func (s *Server) validateHello(request Request) error {
	if request.ProtocolVersion != ProtocolVersion || request.Kind != RequestHello || request.Sequence != 1 {
		return fmt.Errorf("%w: invalid hello envelope", ErrProtocol)
	}
	if request.RequestID == "" {
		return fmt.Errorf("%w: hello request id is empty", ErrProtocol)
	}
	if request.RunID != s.config.RunID || request.AttemptID != s.config.AttemptID {
		return fmt.Errorf("%w: run or attempt identity mismatch", ErrAuthentication)
	}
	if err := model.ValidateID(string(request.SessionID)); err != nil {
		return fmt.Errorf("%w: invalid session id", ErrProtocol)
	}
	if subtle.ConstantTimeCompare([]byte(request.Token), []byte(s.config.Token)) != 1 {
		return ErrAuthentication
	}
	return nil
}

func (s *Server) validateEventRequest(request Request) error {
	if request.ProtocolVersion != ProtocolVersion || request.Kind != RequestEvent || request.Event == nil {
		return fmt.Errorf("%w: invalid event envelope", ErrProtocol)
	}
	if request.RequestID == "" {
		return fmt.Errorf("%w: request id is empty", ErrProtocol)
	}
	if request.RunID != s.config.RunID || request.AttemptID != s.config.AttemptID {
		return fmt.Errorf("%w: run or attempt identity mismatch", ErrAuthentication)
	}
	if request.Event.RunID != request.RunID ||
		request.Event.AttemptID != request.AttemptID ||
		request.Event.SessionID != request.SessionID ||
		request.Event.LocalSequence != request.Sequence {
		return fmt.Errorf("%w: event envelope identity mismatch", ErrProtocol)
	}
	data, err := json.Marshal(request.Event)
	if err != nil {
		return fmt.Errorf("%w: encode event: %v", ErrProtocol, err)
	}
	if len(data) > maxEventPayloadBytes {
		return ErrPayloadTooLarge
	}
	if err := request.Event.Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrProtocol, err)
	}
	return nil
}

func (s *Server) Close() error {
	var closeErr error
	s.closeOnce.Do(func() {
		close(s.done)
		closeErr = s.listener.Close()
		s.mu.Lock()
		for conn := range s.conns {
			_ = conn.Close()
		}
		s.mu.Unlock()
		s.wg.Wait()
		if s.config.Network == "unix" {
			if err := os.Remove(s.config.Address); err != nil && !errors.Is(err, os.ErrNotExist) && closeErr == nil {
				closeErr = err
			}
		}
	})
	return closeErr
}

func (s *Server) Endpoint() (network, address string) {
	return s.config.Network, s.config.Address
}

func (s *Server) Wait(ctx context.Context) error {
	wait := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(wait)
	}()
	select {
	case <-wait:
		return nil
	case <-ctx.Done():
		return context.Cause(ctx)
	}
}
