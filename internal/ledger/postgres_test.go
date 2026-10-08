package ledger

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/abhisek343/cutline/internal/model"
)

func TestPostgresConnectionHandshakeUsesOperationTimeout(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	// Accept a connection without speaking PostgreSQL. A reachable local
	// dependency can stall before the target schedule timeout even starts.
	accepted := make(chan net.Conn, 1)
	go func() {
		conn, err := listener.Accept()
		if err == nil {
			accepted <- conn
		}
	}()
	config := AttemptConfig{
		RunID: model.RunID("run_" + strings.Repeat("a", 32)), AttemptID: model.AttemptID("attempt_" + strings.Repeat("b", 32)),
		CampaignDigest: "sha256:" + strings.Repeat("a", 64), CampaignName: "test", CampaignJSON: []byte(`{}`),
		TargetDigest: "sha256:" + strings.Repeat("b", 64), Adapter: "go-test", AdapterVersion: "native/go-test/1", OperationTimeout: 100 * time.Millisecond,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	started := time.Now()
	store, err := NewPostgres(ctx, "postgres://cutline:invalid@"+listener.Addr().String()+"/cutline?sslmode=disable", config)
	if store != nil {
		store.Close()
		t.Fatal("stalled PostgreSQL handshake returned a store")
	}
	if err == nil || time.Since(started) > 2*time.Second {
		t.Fatalf("operation timeout failed: elapsed=%s err=%v", time.Since(started), err)
	}
	select {
	case conn := <-accepted:
		conn.Close()
	case <-ctx.Done():
		t.Fatal("PostgreSQL connection was not attempted")
	}
}
