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

trap cleanup EXIT
docker compose -f "$compose_file" up --detach --wait
"${GO:-go}" test -v -tags=temporal_integration ./internal/adapters/temporal -run TestLiveFetch -count=1 -timeout=2m
"${GO:-go}" build -buildvcs=false -o ./bin/cutline ./cmd/cutline
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

# Keep the actual failing capsule and machine-readable replay evidence for CI
# upload and local inspection. Minimize exits 2 when a stable violation exists.
mkdir -p artifacts
evidence_dir=$(mktemp -d artifacts/temporal.XXXXXX)
if ./bin/cutline minimize --campaign test/fixtures/temporalcheckout/campaign-faulty.yaml --output "$evidence_dir/capsule" --confirmations 3 --json > "$evidence_dir/minimize.json"; then
	echo "Temporal minimization did not report a violation" >&2
	exit 1
else
	status=$?
	if (( status != 2 )); then
		echo "Temporal minimization exited $status, want 2" >&2
		exit 1
	fi
fi
for attempt in $(seq 1 20); do
	if ./bin/cutline replay "$evidence_dir/capsule" --json > "$evidence_dir/replay-$attempt.json"; then
		status=0
	else
		status=$?
	fi
	# Retain every semantic result and enforce the explicit >=19/20 bound below.
	if (( status != 0 && status != 2 && status != 3 )); then
		echo "Temporal replay $attempt exited $status" >&2
		exit 1
	fi
done
./bin/cutline report "$evidence_dir/capsule" --format json --output "$evidence_dir/report.json"
./bin/cutline report "$evidence_dir/capsule" --format html --output "$evidence_dir/report.html"
python3 - "$evidence_dir" <<'PY'
import html
import json
import pathlib
import sys

root = pathlib.Path(sys.argv[1])
manifest = json.loads((root / "capsule" / "manifest.json").read_text())
assert manifest["adapter"] == "temporal", manifest
assert manifest["stable"] and manifest["reproducedCount"] >= 3, manifest
digest = manifest["failureSignature"]["digest"]
capsule = root / "capsule"
schedule = json.loads((capsule / "schedule.json").read_text())
target = json.loads((capsule / "target.json").read_text())
dependencies = json.loads((capsule / "dependencies.json").read_text())
assert target["digest"] == dependencies["targetDigest"] == manifest["targetDigest"], target
assert target["digest"].startswith("sha256:") and len(target["digest"]) == 71, target
assert target["adapter"] == "temporal" and target["adapterVersion"] == "temporal/1", target
report = json.loads((root / "report.json").read_text())
assert report["signature"]["digest"] == digest, report
assert report["schedule"] == schedule, report
assert report["status"] == "violation", report
assert not report["view"]["issues"] and not report["view"]["incompleteReasons"], report
evaluations = json.loads((capsule / "evaluation.json").read_text())
assert report["evaluations"] == evaluations, report
assert any(e["contract"] == "no-charge-after-cancel" and e["status"] == "violation" for e in evaluations), evaluations
receipts = json.loads((capsule / "effects.json").read_text())
assert len(receipts) == 1, receipts
effects = report["view"]["effects"]
assert len(effects) == 1 and effects[0]["state"] == "committed", effects
assert effects[0]["id"] == receipts[0]["effectId"], effects
assert effects[0]["idempotencyKey"] == receipts[0]["idempotencyKey"] == "temporal-checkout-order-42", effects
rendered = html.unescape((root / "report.html").read_text())
assert digest in rendered and schedule["id"] in rendered, rendered
assert receipts[0]["effectId"] in rendered and "no-charge-after-cancel" in rendered, rendered
exact = 0
for attempt in range(1, 21):
    result = json.loads((root / f"replay-{attempt}.json").read_text())
    assert result["expected"]["digest"] == digest, result
    if result["exact"] and result["status"] == "reproduced":
        assert any(value["digest"] == digest for value in result["observed"]), result
        exact += 1
summary = {"attempts": 20, "exact": exact, "requiredExact": 19, "digest": digest}
(root / "replay-summary.json").write_text(json.dumps(summary, indent=2) + "\n")
assert exact >= 19, summary
print(f"Temporal capsule exact replay: {exact}/20: {digest}")
print(f"Saved evidence: {root}")
PY
