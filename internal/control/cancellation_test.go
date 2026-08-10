package control

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"github.com/abhisek343/cutline/internal/model"
)

func TestDialCancelledBeforeConnectReturnsPromptly(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	started := time.Now()
	_, err := Dial(ctx, ClientConfig{
		Network: "tcp", Address: "127.0.0.1:1", Token: "secret",
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Dial error = %v, want context.Canceled", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("cancelled Dial took %s", elapsed)
	}
}

func TestDialCancellationDuringHandshakeClosesConnection(t *testing.T) {
	t.Parallel()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		defer conn.Close()
		_, _ = io.Copy(io.Discard, conn)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, dialErr := Dial(ctx, ClientConfig{
			Network: "tcp", Address: listener.Addr().String(), Token: "secret",
		})
		result <- dialErr
	}()
	select {
	case err := <-result:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("Dial error = %v, want context deadline", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Dial remained blocked during handshake cancellation")
	}
	select {
	case <-serverDone:
	case <-time.After(2 * time.Second):
		t.Fatal("stalled handshake connection was not cleaned up")
	}
}

func TestSendEventCancellationWithStalledServerIsBounded(t *testing.T) {
	t.Parallel()

	ids := testIDs(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	serverDone := make(chan struct{})
	serverError := make(chan error, 1)
	go func() {
		defer close(serverDone)
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			serverError <- acceptErr
			return
		}
		defer conn.Close()
		decoder := json.NewDecoder(conn)
		encoder := json.NewEncoder(conn)
		var hello Request
		if decodeErr := decoder.Decode(&hello); decodeErr != nil {
			serverError <- decodeErr
			return
		}
		if encodeErr := encoder.Encode(Response{ProtocolVersion: ProtocolVersion, ReplyTo: hello.RequestID}); encodeErr != nil {
			serverError <- encodeErr
			return
		}
		_, _ = io.Copy(io.Discard, conn)
	}()
	client, err := Dial(context.Background(), ClientConfig{
		Network: "tcp", Address: listener.Addr().String(), Token: "secret",
		RunID: ids.run, AttemptID: ids.attempt, SessionID: ids.session,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	_, err = client.SendEvent(ctx, model.Event{
		SchemaVersion: model.EventSchemaVersion, RunID: ids.run, AttemptID: ids.attempt, SessionID: ids.session,
		Type: model.EventTaskStarted, EntityID: mustTestEntity(t, "task"), ObservedAt: time.Now().UTC(),
	})
	cancel()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("SendEvent error = %v, want context deadline", err)
	}
	if closeErr := client.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	select {
	case <-serverDone:
	case <-time.After(2 * time.Second):
		t.Fatal("stalled event server connection was not cleaned up")
	}
	select {
	case serverErr := <-serverError:
		if serverErr != nil && !errors.Is(serverErr, net.ErrClosed) && !errors.Is(serverErr, io.EOF) {
			t.Fatalf("server error = %v", serverErr)
		}
	default:
	}
}

func mustTestEntity(t *testing.T, prefix string) string {
	t.Helper()
	value, err := model.ContentID(prefix, t.Name())
	if err != nil {
		t.Fatal(err)
	}
	return value
}
