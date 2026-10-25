# Observability setup runbook

Covers Grafana Alloy → Grafana Cloud Loki log collection for jobifai.
See [docs/observability.md](../observability.md) for the log event contract.

Story: #110. This story stays open until production ingestion and recovery
evidence are recorded.

---

## 1. Prerequisites

### Grafana Cloud account

1. Create a free Grafana Cloud account at grafana.com/auth/sign-up.
2. In the Cloud Portal: **Connections → Add new connection → Loki**.
3. Note the push URL — it ends with `/loki/api/v1/push`.
4. Create a least-privilege API key: **Access policies → Create access policy**,
   grant only the `logs:write` scope (no `metrics:`, no `traces:`, no admin).
5. Copy the tenant ID (numeric, displayed next to the endpoint URL) and the API key.

### VM environment variables

Add these to `/opt/jobifai/.env` on the VM. This file is loaded by Docker Compose
as `env_file` for the `jobifai` service and as shell environment for the `alloy`
service's passthrough variables.

```
LOG_FORMAT=json
JOBIFAI_ENV=production
GRAFANA_LOKI_URL=https://logs-xxx.grafana.net/loki/api/v1/push
GRAFANA_LOKI_USER=<numeric tenant ID>
GRAFANA_LOKI_API_KEY=<Logs Push API key>
```

**Never** commit `.env` or print its contents. After deploy, verify the variables
are non-empty inside the running containers (not in the file, which can have `KEY=`
with no value):

```bash
# Run from /opt/jobifai/ where docker-compose.yml lives.
# Alloy credentials:
for var in GRAFANA_LOKI_URL GRAFANA_LOKI_USER GRAFANA_LOKI_API_KEY; do
  if docker compose exec -T alloy sh -c "val=\$(printenv ${var}); [ -n \"\$val\" ]" 2>/dev/null; then
    echo "${var}: SET (non-empty)"
  else
    echo "${var}: MISSING or empty"
  fi
done
# App log format (jobifai container):
for var in LOG_FORMAT JOBIFAI_ENV; do
  if docker compose exec -T jobifai sh -c "val=\$(printenv ${var}); [ -n \"\$val\" ]" 2>/dev/null; then
    echo "${var}: SET (non-empty)"
  else
    echo "${var}: MISSING or empty"
  fi
done
```

### Compose project name pre-flight check

`docker-compose.yml` declares `name: jobifai`. Before the first deploy that adds
Alloy, verify this matches the running project name to avoid renaming existing
networks and volumes. SSH to the VM and run:

```bash
# Inspect the com.docker.compose.project label on running containers.
docker ps --format '{{.Label "com.docker.compose.project"}}' | sort -u | grep -v '^$'
```

All running containers must report `jobifai`. If any show a different project name,
stop here and reconcile before proceeding.

Also verify that the expected named volumes already exist under the `jobifai_` prefix:

```bash
docker volume ls --filter "label=com.docker.compose.project=jobifai" \
  --format '{{.Name}}'
```

Expected volumes (after initial deploy without Alloy): `jobifai_caddy_data`,
`jobifai_caddy_config`. If volumes with different names exist, adding `name: jobifai`
will create new empty volumes instead of reusing them — stop and reconcile first.

---

## 2. Validate configs

Run these locally before every deploy. Placeholder env var values are sufficient;
they are only used by the validator and are not transmitted anywhere.

```bash
# Production config
docker run --rm \
  -e GRAFANA_LOKI_URL="https://logs-placeholder.grafana.net/loki/api/v1/push" \
  -e GRAFANA_LOKI_USER="0" \
  -e GRAFANA_LOKI_API_KEY="placeholder" \
  -v "$(pwd)/infra/gcp/observability/alloy/config.alloy:/etc/alloy/config.alloy:ro" \
  grafana/alloy:v1.21.2 validate /etc/alloy/config.alloy

# Local smoke-test config
docker run --rm \
  -v "$(pwd)/infra/observability/alloy/config.local.alloy:/etc/alloy/config.alloy:ro" \
  grafana/alloy:v1.21.2 validate /etc/alloy/config.alloy
```

Both commands should exit 0 with no output.

---

## 3. Deploy

Always deploy from `main`:

```bash
git checkout main && git pull origin main
make deploy-gcp
```

`deploy.sh` builds the image, pushes it, syncs `docker-compose.yml`, `Caddyfile`,
and `observability/` to the VM, then pulls all services and runs `docker compose up -d`.

After deploy, confirm Alloy started:

```bash
gcloud compute ssh jobifai-vm --zone=australia-southeast1-a -- \
  "curl -sf http://127.0.0.1:12345/-/ready && echo 'alloy ready'"
```

`/-/ready` reflects process readiness only — it does not confirm Loki delivery.
Prove delivery separately by checking Grafana after a bot run (§4).

Do not run `docker compose config` with real secrets in the environment; this
would print resolved credentials to the terminal.

---

## 4. Production validation (after a real bot run)

Open Explore in your Grafana Cloud instance. These queries require at least one
completed bot run since deploy.

### All logs for a run

```logql
{service="jobifai", env="production"} | json | automation_run_id="<run_id>"
```

### Lifecycle events only

```logql
{service="jobifai", env="production"} | json | automation_run_id="<run_id>" | event != ""
```

Check that `run_start` → `attempt_result` → `run_end` all appear. The
`attempt_result` line carries `duration_ms` and `outcome`.

### Redaction targeted check

```logql
{service="jobifai"} |~ "(?i)(trackingId=|password=|Authorization:|Bearer )"
```

This targets the most likely secret patterns; it is a targeted check, not a
guarantee of all possible secret absence. See [docs/observability.md](../observability.md)
for the full sensitive-data handling policy.

---

## 5. Alloy UI access

Alloy listens on `0.0.0.0:12345` inside the container. The port is published only
on the VM loopback (`127.0.0.1:12345`); it is not reachable from the internet.

To access the UI from your laptop, open an SSH tunnel:

```bash
gcloud compute ssh jobifai-vm --zone=australia-southeast1-a -- -L 12345:127.0.0.1:12345 -N
```

Then open `http://localhost:12345` in a browser. The Pipeline graph shows live
delivery metrics including retry attempts and drops.

---

## 6. Local smoke test

Requires Docker. Does not require Grafana Cloud credentials.

```bash
make obs-up
# Start the app and pipe its output to the log file Alloy tails.
LOG_FORMAT=json JOBIFAI_ENV=local-smoke make run 2>&1 | tee /tmp/jobifai-smoke.log
```

Open `http://127.0.0.1:3000` (Grafana, anonymous admin) → Explore → select
**Loki** datasource → run `{service="jobifai"}`.

Alloy UI: `http://127.0.0.1:12345`.

To stop the stack:

```bash
# Ctrl-C the make run process first, then:
make obs-down
```

**How the file mount works**: `docker-compose.observability.yml` bind-mounts the
host `/tmp` directory into the Alloy container as `/var/log/host-tmp:ro`. Alloy
tails `/var/log/host-tmp/jobifai-smoke.log`. This path works on macOS and Linux.

---

## 7. Recovery tests

### Test A — Alloy restart (positions file)

Tests that Alloy resumes from saved positions after a restart.
**Saved positions track reading, not Loki acknowledgement.** Lines already read
into memory but not yet acknowledged at the time of the restart may be lost.

```bash
# 1. Start local stack and confirm logs are flowing.
make obs-up
LOG_FORMAT=json JOBIFAI_ENV=local-smoke make run 2>&1 | tee /tmp/jobifai-smoke.log &
sleep 5  # let a few lines arrive in Loki

# 2. Note a unique marker already in Loki (search in Grafana for a recent log_id).

# 3. Capture marker and time window, then stop Alloy.
MARKER="recovery-test-A-$(date +%s)"
START=$(date -u +%s)000000000
docker compose -f infra/observability/docker-compose.observability.yml stop alloy

# 4. Emit identifiable lines while Alloy is stopped.
echo '{"time":"'"$(date -u +%Y-%m-%dT%H:%M:%SZ)"'","level":"info","message":"test-recovery-A","marker":"'"$MARKER"'","service":"jobifai","env":"local-smoke"}' \
  >> /tmp/jobifai-smoke.log

# 5. Restart Alloy.
docker compose -f infra/observability/docker-compose.observability.yml start alloy
sleep 10  # allow Alloy to resume and flush

# 6. Query Loki for the marker.
END=$(($(date -u +%s) + 60))000000000
curl -sG "http://127.0.0.1:3100/loki/api/v1/query_range" \
  --data-urlencode "query={service=\"jobifai\"} |= \"${MARKER}\"" \
  --data-urlencode "start=${START}" \
  --data-urlencode "end=${END}" \
  --data-urlencode 'limit=10' | python3 -c "
import sys, json
MARKER = '${MARKER}'
d = json.load(sys.stdin)
streams = d.get('data', {}).get('result', [])
lines = [v[1] for r in streams for v in r.get('values', [])]
found = [l for l in lines if MARKER in l]
print(f'Lines in Loki with marker: {len(found)} (expected: 1)')
assert len(found) >= 1, 'FAIL: marker not found'
print('PASS')
"
```

Expected: the marker line appears in Loki because Alloy resumed from the saved
position. Lines that were in Alloy's internal buffer at stop time may be absent.

### Test B — Backend connectivity interruption (Loki stop/start, Alloy stays running)

Tests Alloy's retry behavior when the backend is temporarily unreachable.
**Do not restart Alloy during this test** — restarting discards its memory and
tests a different failure mode.

```bash
# 1. Start local stack and confirm logs are flowing.
make obs-up
LOG_FORMAT=json JOBIFAI_ENV=local-smoke make run 2>&1 | tee /tmp/jobifai-smoke.log &
sleep 5

# 2. Capture marker and time window, then stop Loki (Alloy keeps running).
MARKER="recovery-test-B-$(date +%s)"
START=$(date -u +%s)000000000
docker compose -f infra/observability/docker-compose.observability.yml stop loki

# 3. Emit identifiable lines while Loki is unavailable.
for i in $(seq 1 5); do
  echo '{"time":"'"$(date -u +%Y-%m-%dT%H:%M:%SZ)"'","level":"info","message":"test-recovery-B","marker":"'"$MARKER"'","seq":'"$i"',"service":"jobifai","env":"local-smoke"}' \
    >> /tmp/jobifai-smoke.log
  sleep 2
done

# 4. Watch Alloy retry (optional — Alloy UI at http://127.0.0.1:12345).

# 5. Restore Loki.
docker compose -f infra/observability/docker-compose.observability.yml start loki
sleep 30  # allow Alloy to detect Loki is back and flush its queue

# 6. Query for the marker.
END=$(($(date -u +%s) + 60))000000000
curl -sG "http://127.0.0.1:3100/loki/api/v1/query_range" \
  --data-urlencode "query={service=\"jobifai\"} |= \"${MARKER}\"" \
  --data-urlencode "start=${START}" \
  --data-urlencode "end=${END}" \
  --data-urlencode 'limit=20' | python3 -c "
import sys, json
MARKER = '${MARKER}'
d = json.load(sys.stdin)
streams = d.get('data', {}).get('result', [])
lines = [v[1] for r in streams for v in r.get('values', [])]
found = [l for l in lines if MARKER in l]
seqs = sorted(json.loads(l).get('seq', '?') for l in found)
print(f'Lines with marker: {len(found)}/5 (expected: 5), seqs={seqs}')
assert len(found) == 5, f'FAIL: expected 5 lines, got {len(found)}'
assert seqs == [1, 2, 3, 4, 5], f'FAIL: wrong sequences, got {seqs}'
print('PASS')
"
```

Expected: 5 lines found (Alloy retried and delivered once Loki came back).
If lines are missing, check the Alloy UI Pipeline graph for drop events. Drops
occur only after retry exhaustion (10 attempts, 500 ms → 5 min backoff).

### Production connectivity interruption (pending — requires VM access)

Tests Alloy's retry behavior on production without restarting it (restart discards
the in-memory buffer and tests a different failure mode). Use Docker network
disconnect to sever Alloy's network access while keeping the process running.

```bash
# Run from /opt/jobifai/ on the VM.
ALLOY_ID=$(docker compose ps -q alloy)

# 1. Disconnect Alloy from the production network (keeps process running).
docker network disconnect jobifai_edge "$ALLOY_ID"

# 2. Emit uniquely identifiable lines via the app or by appending to a known log path.
#    Alloy reads them but cannot deliver while disconnected.

# 3. Restore network access.
docker network connect jobifai_edge "$ALLOY_ID"

# 4. Wait for Alloy to reconnect and flush (first retry fires after 500 ms).
sleep 30

# 5. Verify delivery in Grafana Cloud:
#    {service="jobifai", env="production"} |= "<unique marker>"
#    — lines emitted in step 2 should appear.
```

Rollback: `docker network connect jobifai_edge "$ALLOY_ID"` is the only cleanup
needed. The app (`jobifai`) and `caddy` containers are never touched.

---

## 8. Resource and limit summary

| Component | Limit | Notes |
|-----------|-------|-------|
| Alloy RAM | 256 MB | OOMKilled if exceeded; raise `mem_limit` if needed |
| Alloy CPU | 0.25 vCPU | Sufficient for one log stream |
| Docker log buffer | 500 MB (5 × 100 MB) | Rotated logs are unrecoverable |
| Retry attempts | 10 | Backoff 500 ms → 5 min; batch dropped on exhaustion |
| Grafana Free ingestion | 50 GB / month | Excess → 429; no auto paid upgrade |
| Grafana Free retention | 14 days | Verified October 2026, grafana.com/pricing |

---

## 9. Troubleshooting

| Symptom | Likely cause |
|---------|-------------|
| 401 from Loki | Wrong API key or wrong scope (need `logs:write`, not admin key) |
| 404 from Loki | `GRAFANA_LOKI_URL` does not end with `/loki/api/v1/push` |
| Container not discovered | Compose project label mismatch — run `docker ps --format '{{.Label "com.docker.compose.project"}}' \| sort -u` on the VM; must show only `jobifai` |
| `/-/ready` returns 503 | Alloy has not finished startup; retry after a few seconds |
| No data in Grafana | Check time range, Loki datasource URL, Alloy UI Pipeline graph at `http://127.0.0.1:12345` |
| `GRAFANA_LOKI_USER` wrong | Must be the **numeric** tenant ID, not the org slug |
| Alloy exits with OOM | Raise `mem_limit: 256m` in `docker-compose.yml` |

---

## 10. Handoff to #111–#114

| Detail | Value |
|--------|-------|
| Grafana datasource name | Grafana Cloud Loki (or the provisioned name in Grafana) |
| Stream selector | `{service="jobifai", env="production"}` |
| High-cardinality ID fields | `automation_run_id`, `user_id`, `log_id`, `job_id` (in JSON body) |
| Lifecycle event field | `event` (values: `run_start`, `attempt_result`, `run_end`, etc.) |
| Schema version field | `schema_version: "1"` on all lifecycle events |
