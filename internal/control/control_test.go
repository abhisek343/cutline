package control

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/abhisek343/cutline/internal/model"
)

type recordingHandler struct {
	mu       sync.Mutex
	sessions []Hello
	events   []model.Event
	action   Action
}

func (h *recordingHandler) SessionStarted(hello Hello) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.sessions = append(h.sessions, hello)
	return nil
}

func (h *recordingHandler) EventAccepted(event model.Event) (Decision, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.events = append(h.events, event)
	return Decision{Action: h.action}, nil
}

func TestClientServerRoundTripAndConcurrentCalls(t *testing.T) {
	t.Parallel()

	ids := testIDs(t)
	handler := &recordingHandler{action: ActionRelease}
	server, err := Listen(ServerConfig{
		Network: "tcp", Address: "127.0.0.1:0", Token: "secret",
		RunID: ids.run, AttemptID: ids.attempt, Handler: handler,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	network, address := server.Endpoint()
	client, err := Dial(context.Background(), ClientConfig{
		Network: network, Address: address, Token: "secret",
		RunID: ids.run, AttemptID: ids.attempt, SessionID: ids.session,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })

	const count = 32
	errs := make(chan error, count)
	var group sync.WaitGroup
	for index := range count {
		group.Add(1)
		go func() {
			defer group.Done()
			entity, _ := model.ContentID("task", t.Name(), string(rune(index)))
			event := model.Event{
				SchemaVersion: model.EventSchemaVersion,
				RunID:         ids.run, AttemptID: ids.attempt, SessionID: ids.session,
				Type: model.EventTaskStarted, EntityID: entity, ObservedAt: time.Now().UTC(),
			}
			decision, err := client.SendEvent(context.Background(), event)
			if err == nil && decision.Action != ActionRelease {
				err = errors.New("unexpected scheduler action")
			}
			errs <- err
		}()
	}
	group.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("SendEvent() error = %v", err)
		}
	}

	handler.mu.Lock()
	defer handler.mu.Unlock()
	if len(handler.sessions) != 1 || len(handler.events) != count {
		t.Fatalf("sessions=%d events=%d", len(handler.sessions), len(handler.events))
	}
	for index, event := range handler.events {
		if event.LocalSequence != uint64(index+2) {
			t.Fatalf("event %d local sequence = %d", index, event.LocalSequence)
		}
	}
}

func TestAuthenticationFailure(t *testing.T) {
	t.Parallel()

	ids := testIDs(t)
	server, err := Listen(ServerConfig{
		Network: "tcp", Address: "127.0.0.1:0", Token: "correct",
		RunID: ids.run, AttemptID: ids.attempt, Handler: &recordingHandler{},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	network, address := server.Endpoint()
	_, err = Dial(context.Background(), ClientConfig{
		Network: network, Address: address, Token: "wrong",
		RunID: ids.run, AttemptID: ids.attempt, SessionID: ids.session,
	})
	if !errors.Is(err, ErrAuthentication) {
		t.Fatalf("Dial() error = %v, want ErrAuthentication", err)
	}
}

func TestServerRejectsSequenceGap(t *testing.T) {
	t.Parallel()

	ids := testIDs(t)
	server, err := Listen(ServerConfig{
		Network: "tcp", Address: "127.0.0.1:0", Token: "secret",
		RunID: ids.run, AttemptID: ids.attempt, Handler: &recordingHandler{},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	network, address := server.Endpoint()
	conn, err := net.Dial(network, address)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	encoder := json.NewEncoder(conn)
	decoder := json.NewDecoder(conn)
	hello := Request{
		ProtocolVersion: ProtocolVersion, RequestID: "hello", Sequence: 1, Kind: RequestHello,
		Token: "secret", RunID: ids.run, AttemptID: ids.attempt, SessionID: ids.session,
	}
	if err := encoder.Encode(hello); err != nil {
		t.Fatal(err)
	}
	var response Response
	if err := decoder.Decode(&response); err != nil {
		t.Fatal(err)
	}
	entity, _ := model.ContentID("task", t.Name())
	event := model.Event{
		SchemaVersion: model.EventSchemaVersion,
		RunID:         ids.run, AttemptID: ids.attempt, SessionID: ids.session,
		LocalSequence: 3, Type: model.EventTaskStarted, EntityID: entity, ObservedAt: time.Now().UTC(),
	}
	if err := encoder.Encode(Request{
		ProtocolVersion: ProtocolVersion, RequestID: "gap", Sequence: 3, Kind: RequestEvent,
		RunID: ids.run, AttemptID: ids.attempt, SessionID: ids.session, Event: &event,
	}); err != nil {
		t.Fatal(err)
	}
	if err := decoder.Decode(&response); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(response.Error, ErrSequence.Error()) {
		t.Fatalf("response error = %q", response.Error)
	}
}

type idSet struct {
	run     model.RunID
	attempt model.AttemptID
	session model.SessionID
}

func testIDs(t *testing.T) idSet {
	t.Helper()
	run, _ := model.ContentID("run", t.Name())
	attempt, _ := model.ContentID("attempt", t.Name())
	session, _ := model.ContentID("session", t.Name())
	return idSet{model.RunID(run), model.AttemptID(attempt), model.SessionID(session)}
}
