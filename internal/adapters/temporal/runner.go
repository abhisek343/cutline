package temporal

import (
	"context"
	"fmt"
	"net"
	"strings"

	"github.com/abhisek343/cutline/internal/adapters/native"
	"github.com/abhisek343/cutline/internal/campaign"
	"github.com/abhisek343/cutline/internal/capsule"
	"github.com/abhisek343/cutline/internal/explorer"
	"github.com/abhisek343/cutline/internal/minimize"
	"github.com/abhisek343/cutline/internal/replay"
	"github.com/abhisek343/cutline/internal/signature"
)

// Runner executes a local Temporal campaign. The target command is responsible
// for starting an instrumented worker and workflow; Cutline supplies a private
// control endpoint plus the local Temporal connection settings for that run.
type Runner struct {
	BaseDirectory string
	TempDirectory string
}

func (r Runner) Run(ctx context.Context, spec campaign.Campaign) (native.Result, error) {
	prepared, inner, err := r.prepare(spec)
	if err != nil {
		return native.Result{}, err
	}
	return inner.Run(ctx, prepared)
}

func (r Runner) Discover(ctx context.Context, spec campaign.Campaign) (native.Result, explorer.Discovery, error) {
	prepared, inner, err := r.prepare(spec)
	if err != nil {
		return native.Result{}, explorer.Discovery{}, err
	}
	return inner.Discover(ctx, prepared)
}

func (r Runner) RunSchedule(ctx context.Context, spec campaign.Campaign, schedule explorer.Schedule) (native.Result, error) {
	prepared, inner, err := r.prepare(spec)
	if err != nil {
		return native.Result{}, err
	}
	return inner.RunSchedule(ctx, prepared, schedule)
}

// RunScheduleWithProvenance captures the actual target build around execution
// using the original Temporal descriptor rather than its protocol delegate.
func (r Runner) RunScheduleWithProvenance(ctx context.Context, spec campaign.Campaign, schedule explorer.Schedule) (native.Result, error) {
	before, err := spec.ExecutionDigest(ctx, r.BaseDirectory)
	if err != nil {
		return native.Result{}, err
	}
	result, err := r.RunSchedule(ctx, spec, schedule)
	if err != nil {
		return result, err
	}
	after, err := spec.ExecutionDigest(ctx, r.BaseDirectory)
	if err != nil {
		return result, err
	}
	if before != after {
		return result, fmt.Errorf("target build changed during final execution")
	}
	result.TargetDigest = after
	return result, nil
}

func (r Runner) RunCampaign(ctx context.Context, spec campaign.Campaign) (native.CampaignResult, error) {
	prepared, inner, err := r.prepare(spec)
	if err != nil {
		return native.CampaignResult{}, err
	}
	return inner.RunCampaign(ctx, prepared)
}

func (r Runner) MinimizeSchedule(ctx context.Context, spec campaign.Campaign, schedule explorer.Schedule, target signature.Signature, config minimize.Config) (minimize.Result, error) {
	prepared, inner, err := r.prepare(spec)
	if err != nil {
		return minimize.Result{}, err
	}
	return inner.MinimizeSchedule(ctx, prepared, schedule, target, config)
}

func (r Runner) BuildCapsule(spec campaign.Campaign, schedule explorer.Schedule, result native.Result, minimization minimize.Result, destination string) (capsule.Manifest, error) {
	_, inner, err := r.prepare(spec)
	if err != nil {
		return capsule.Manifest{}, err
	}
	return inner.BuildCapsule(spec, schedule, result, minimization, destination)
}

func (r Runner) ReplayCapsule(ctx context.Context, root string, config replay.Config) (replay.Result, error) {
	if config.BaseDirectory == "" {
		config.BaseDirectory = r.BaseDirectory
	}
	if config.AdapterVersion == "" {
		config.AdapterVersion = AdapterMajorVersion
	}
	return replay.Run(ctx, root, config, func(candidate context.Context, spec campaign.Campaign, schedule explorer.Schedule) ([]signature.Signature, error) {
		result, err := r.RunSchedule(candidate, spec, schedule)
		if err != nil {
			return nil, err
		}
		return result.Signatures, nil
	})
}

func (r Runner) prepare(spec campaign.Campaign) (campaign.Campaign, native.Runner, error) {
	if spec.Target.Adapter != "temporal" {
		return campaign.Campaign{}, native.Runner{}, fmt.Errorf("Temporal runner requires target.adapter temporal")
	}
	address := strings.TrimSpace(spec.Target.Temporal.Address)
	if address == "" {
		address = "127.0.0.1:7233"
	}
	host, _, err := net.SplitHostPort(address)
	if err != nil || host == "" {
		return campaign.Campaign{}, native.Runner{}, fmt.Errorf("Temporal campaign address must be host:port: %q", address)
	}
	if host != "localhost" {
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() {
			return campaign.Campaign{}, native.Runner{}, fmt.Errorf("Temporal campaign address must be loopback, got %q", host)
		}
	}
	namespace := strings.TrimSpace(spec.Target.Temporal.Namespace)
	if namespace == "" {
		namespace = "default"
	}
	prepared := spec
	prepared.Target.Adapter = "go-command"
	inner := native.Runner{
		BaseDirectory:  r.BaseDirectory,
		TempDirectory:  r.TempDirectory,
		AdapterVersion: AdapterMajorVersion,
		Environment: map[string]string{
			"CUTLINE_TEMPORAL_ADDRESS":   address,
			"CUTLINE_TEMPORAL_NAMESPACE": namespace,
			"CUTLINE_TEMPORAL_CAMPAIGN":  "1",
		},
	}
	return prepared, inner, nil
}
