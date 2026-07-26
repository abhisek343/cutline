package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strings"

	"github.com/abhisek343/cutline/internal/adapters/native"
	"github.com/abhisek343/cutline/internal/adapters/temporal"
	"github.com/abhisek343/cutline/internal/buildinfo"
	"github.com/abhisek343/cutline/internal/campaign"
	"github.com/abhisek343/cutline/internal/ingest"
	"github.com/abhisek343/cutline/internal/ledger"
	"github.com/abhisek343/cutline/internal/model"
	"github.com/jackc/pgx/v5/pgxpool"
)

const postgresDSNEnvironment = "CUTLINE_POSTGRES_DSN"

func runnerForCampaign(spec campaign.Campaign, baseDirectory string) (native.Runner, error) {
	dsn := strings.TrimSpace(os.Getenv(postgresDSNEnvironment))
	runner := native.Runner{BaseDirectory: baseDirectory}
	if spec.Target.Adapter == "temporal" {
		address := strings.TrimSpace(spec.Target.Temporal.Address)
		if address == "" {
			address = "127.0.0.1:7233"
		}
		if err := validateLocalTemporalAddress(address); err != nil {
			return native.Runner{}, err
		}
		namespace := strings.TrimSpace(spec.Target.Temporal.Namespace)
		if namespace == "" {
			namespace = "default"
		}
		runner.AdapterVersion = temporal.AdapterMajorVersion
		runner.Environment = map[string]string{"CUTLINE_TEMPORAL_ADDRESS": address, "CUTLINE_TEMPORAL_NAMESPACE": namespace, "CUTLINE_TEMPORAL_CAMPAIGN": "1"}
	}
	if dsn == "" {
		return runner, nil
	}
	if err := validateLocalPostgresDSN(dsn); err != nil {
		return native.Runner{}, err
	}
	campaignDigest, err := spec.Digest()
	if err != nil {
		return native.Runner{}, err
	}
	targetDigest, err := spec.TargetDigest()
	if err != nil {
		return native.Runner{}, err
	}
	campaignJSON, err := json.Marshal(spec)
	if err != nil {
		return native.Runner{}, fmt.Errorf("marshal campaign for PostgreSQL: %w", err)
	}
	info := buildinfo.Current()
	runner.StoreFactory = func(ctx context.Context, runID model.RunID, attemptID model.AttemptID, maxEvents int) (ingest.Store, error) {
		return ledger.NewPostgres(ctx, dsn, ledger.AttemptConfig{
			RunID: runID, AttemptID: attemptID,
			CampaignDigest: campaignDigest, CampaignName: spec.Name, CampaignJSON: campaignJSON,
			TargetDigest: targetDigest, Adapter: spec.Target.Adapter, AdapterVersion: info.Version,
			TargetMetadata: map[string]any{"command": spec.Target.Command, "workingDirectory": spec.Target.WorkingDirectory, "temporal": spec.Target.Temporal},
			Seed:           spec.Exploration.Seed, MaxEvents: maxEvents,
			OperationTimeout: spec.Exploration.DrainTimeout.Value(),
		})
	}
	return runner, nil
}

func validateLocalTemporalAddress(address string) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil || host == "" {
		return fmt.Errorf("Temporal address must be host:port: %q", address)
	}
	if strings.EqualFold(host, "localhost") {
		return nil
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("Temporal address must be loopback, got %q", host)
	}
	return nil
}

func validateLocalPostgresDSN(dsn string) error {
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return fmt.Errorf("invalid %s: %w", postgresDSNEnvironment, err)
	}
	host := strings.TrimSpace(config.ConnConfig.Host)
	if host == "" || strings.HasPrefix(host, "/") || strings.EqualFold(host, "localhost") {
		return nil
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return nil
	}
	return fmt.Errorf("%s must point to a loopback host or local Unix socket", postgresDSNEnvironment)
}
