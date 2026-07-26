#!/usr/bin/env bash
set -euo pipefail

compose_file="test/temporal/docker-compose.yml"
cleanup() {
	status=$?
	if (( status != 0 )); then
		docker compose -f "$compose_file" logs --no-color || true
	fi
	docker compose -f "$compose_file" down --volumes --remove-orphans || true
	exit "$status"
}

docker compose -f "$compose_file" up --detach --wait
trap cleanup EXIT
"${GO:-go}" test -tags=temporal_integration ./internal/adapters/temporal -run TestLiveFetchReadsCanceledWorkflowFromTemporalServer -count=1 -timeout=2m
"${GO:-go}" build -o ./bin/cutline ./cmd/cutline
./bin/cutline run --campaign test/fixtures/temporalcheckout/campaign-clean.yaml
if ./bin/cutline run --campaign test/fixtures/temporalcheckout/campaign-faulty.yaml; then
	echo "faulty Temporal campaign passed unexpectedly" >&2
	exit 1
else
	status=$?
	if (( status != 2 )); then
		echo "faulty Temporal campaign exited $status, want 2" >&2
		exit 1
	fi
fi
