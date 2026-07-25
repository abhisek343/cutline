package fixtureledger

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

var ErrInvalidRecord = errors.New("invalid fixture ledger record")

// Record is an authoritative outcome emitted by a disposable benchmark
// dependency, independently from the Cutline SDK event stream.
type Record struct {
	EffectID       string    `json:"effectId"`
	Kind           string    `json:"kind"`
	IdempotencyKey string    `json:"idempotencyKey,omitempty"`
	CommittedAt    time.Time `json:"committedAt"`
	Source         string    `json:"source"`
}

func (r Record) Validate() error {
	if r.EffectID == "" || r.Kind == "" || r.Source == "" || r.CommittedAt.IsZero() {
		return ErrInvalidRecord
	}
	return nil
}

// Append durably records one fixture effect. The benchmark process owns its
// ledger path, so appends use restrictive permissions and explicit Sync.
func Append(path string, record Record) error {
	if err := record.Validate(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create fixture ledger directory: %w", err)
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open fixture ledger: %w", err)
	}
	encoder := json.NewEncoder(file)
	if err := encoder.Encode(record); err != nil {
		_ = file.Close()
		return fmt.Errorf("encode fixture ledger: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return fmt.Errorf("sync fixture ledger: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close fixture ledger: %w", err)
	}
	return nil
}

func Read(path string, maxRecords int) ([]Record, error) {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return []Record{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open fixture ledger: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 4096), 64<<10)
	records := make([]Record, 0)
	for scanner.Scan() {
		if maxRecords > 0 && len(records) >= maxRecords {
			return nil, fmt.Errorf("fixture ledger exceeds %d records", maxRecords)
		}
		var record Record
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			return nil, fmt.Errorf("decode fixture ledger record %d: %w", len(records)+1, err)
		}
		if err := record.Validate(); err != nil {
			return nil, fmt.Errorf("validate fixture ledger record %d: %w", len(records)+1, err)
		}
		records = append(records, record)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan fixture ledger: %w", err)
	}
	return records, nil
}
