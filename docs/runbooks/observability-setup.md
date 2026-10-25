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

**Never** commit `.env` or print its contents. To verify the variables are set
without revealing values, SSH to the VM and run:

```bash
for var in LOG_FORMAT JOBIFAI_ENV GRAFANA_LOKI_URL GRAFANA_LOKI_USER GRAFANA_LOKI_API_KEY; do
  if grep -q "^${var}=" /opt/jobifai/.env 2>/dev/null; then
    echo "${var}: SET"
  else
    echo "${var}: MISSING"
  fi
done
```

### Compose project name pre-flight check

`docker-compose.yml` declares `name: jobifai`. Before the first deploy that adds
Alloy, verify this matches the running project name to avoid renaming existing
networks and volumes. SSH to the VM and run:

```bash
docker ps --format '{{.Names}}' | grep -oE '^[^-]+' | sort -u
```

If the output contains `jobifai`, the project name already matches and the deploy
is safe. If it shows a different prefix (e.g. the directory name), stop here and
reconcile before proceeding.

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
# 3. Stop Alloy (not the whole stack).
docker compose -f infra/observability/docker-compose.observability.yml stop alloy

# 4. Emit identifiable lines while Alloy is stopped.
echo '{"time":"'"$(date -u +%Y-%m-%dT%H:%M:%SZ)"'","level":"info","message":"test-recovery-A","marker":"recovery-test-A-'"$(date +%s)"'","service":"jobifai","env":"local-smoke"}' \
  >> /tmp/jobifai-smoke.log

# 5. Restart Alloy.
docker compose -f infra/observability/docker-compose.observability.yml start alloy
sleep 10  # allow Alloy to resume and flush

# 6. Query Loki for the marker.
curl -sG "http://127.0.0.1:3100/loki/api/v1/query" \
  --data-urlencode 'query={service="jobifai"} |= "recovery-test-A"' \
  --data-urlencode 'limit=5' | python3 -m json.tool | grep -A1 '"values"'
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

# 2. Stop Loki (Alloy keeps running).
docker compose -f infra/observability/docker-compose.observability.yml stop loki

# 3. Emit identifiable lines while Loki is unavailable.
MARKER="recovery-test-B-$(date +%s)"
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
curl -sG "http://127.0.0.1:3100/loki/api/v1/query" \
  --data-urlencode 'query={service="jobifai"} |= "'"$MARKER"'"' \
  --data-urlencode 'limit=10' | python3 -m json.tool | grep -c '"ts"'
```

Expected: 5 lines found (Alloy retried and delivered once Loki came back).
If lines are missing, check the Alloy UI Pipeline graph for drop events. Drops
occur only after retry exhaustion (10 attempts, 500 ms → 5 min backoff).

### Production connectivity interruption (pending — requires VM access)

When VM access is available, test Alloy-scoped interruption without touching the
app or VM-wide networking:

```bash
# On the VM only — scoped to Alloy's outbound connectivity.
# Option 1: temporarily point GRAFANA_LOKI_URL at a non-existent endpoint,
#            then restart only Alloy (not the app).
OLD_URL=$(grep GRAFANA_LOKI_URL /opt/jobifai/.env | cut -d= -f2-)
sed -i 's|GRAFANA_LOKI_URL=.*|GRAFANA_LOKI_URL=https://invalid.example.com/loki/api/v1/push|' /opt/jobifai/.env
docker compose -f /opt/jobifai/docker-compose.yml restart alloy
# ... emit logs, observe retries via tunnel to http://127.0.0.1:12345 ...
# Restore:
sed -i "s|GRAFANA_LOKI_URL=.*|GRAFANA_LOKI_URL=${OLD_URL}|" /opt/jobifai/.env
docker compose -f /opt/jobifai/docker-compose.yml restart alloy
```

Rollback: restoring the URL and restarting Alloy is the full cleanup. The app
continues running without interruption throughout.

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
| Container not discovered | Compose project label mismatch — check `docker ps --format '{{.Names}}'` prefix; must be `jobifai` |
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
