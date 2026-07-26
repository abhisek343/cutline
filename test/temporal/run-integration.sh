#!/usr/bin/env bash
set -euo pipefail

compose_file="test/temporal/docker-compose.yml"
cleanup() {
  docker compose -f "$compose_file" down --volumes --remove-orphans
}

docker compose -f "$compose_file" up --detach --wait
trap cleanup EXIT
"${GO:-go}" test -tags=temporal_integration ./internal/adapters/temporal -run TestLiveFetchReadsCanceledWorkflowFromTemporalServer -count=1 -timeout=2m
