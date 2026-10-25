# Jobifai on Google Cloud (project `jobifai-494014`)

Production host: **https://jobifai.com.au** (GoDaddy DNS → static IP).

DealFindio will be added later on the same VM (see commented blocks in `Caddyfile` / compose).

## Deploy latest version (Mac → GCP)

**Production always deploys from `main`**, clean and matching `origin/main`. Merge and push feature branches first; your checkout can be on any branch while developing, but run deploy from `main`:

```bash
git checkout main
git pull origin main
make deploy-gcp
```

Or:

```bash
chmod +x infra/gcp/deploy.sh
./infra/gcp/deploy.sh
```

This will:

1. Build the Docker image for **linux/amd64** (GCP VM, not Apple Silicon).
2. Push to Artifact Registry.
3. Copy `docker-compose.yml` + `Caddyfile` to the VM.
4. `docker compose pull` + `up -d` on `jobifai-vm`.

**Rollout only** (image already pushed):

```bash
make deploy-gcp-rollout
# or: ./infra/gcp/deploy.sh --rollout-only
```

**Build and push only** (no VM restart):

```bash
./infra/gcp/deploy.sh --build-only
```

Optional overrides: copy `infra/gcp/deploy.env.example` → `infra/gcp/deploy.env`.

Requires: `gcloud`, `docker`, logged in (`gcloud auth login`), Docker buildx.

## Current GCP state (from project audit)

| Item | Value |
|------|--------|
| Project ID | `jobifai-494014` |
| Region | `australia-southeast1` |
| Static IP | `34.87.215.145` (reserved: `jobifai-static-ip`) |
| VM | **Not created yet** — target `jobifai-vm`, `e2-standard-2`, 100 GB boot disk |
| Registry (to create) | `australia-southeast1-docker.pkg.dev/jobifai-494014/jobifai` |
| Credit | ~$417 USD, trial expires **2026-12-21** |
| Est. cost | ~**$59 USD / ~$87 AUD** per month (VM 24/7) |

## 1. One-time setup (run locally with `gcloud`)

```bash
gcloud config set project jobifai-494014

gcloud services enable \
  artifactregistry.googleapis.com \
  cloudbuild.googleapis.com \
  secretmanager.googleapis.com \
  compute.googleapis.com

gcloud artifacts repositories create jobifai \
  --repository-format=docker \
  --location=australia-southeast1 \
  --description="Jobifai Docker images"

gcloud compute firewall-rules create allow-http \
  --allow=tcp:80 \
  --target-tags=caddy-server \
  --description="HTTP for ACME + redirect to HTTPS (Caddy)"

gcloud compute firewall-rules create allow-https \
  --allow=tcp:443 \
  --target-tags=caddy-server \
  --description="HTTPS to Caddy on Jobifai VM"

# Optional but recommended: restrict SSH to your IP (replace YOUR_IP)
# gcloud compute firewall-rules create allow-ssh-admin \
#   --allow=tcp:22 --target-tags=caddy-server --source-ranges=YOUR_IP/32

gcloud compute instances create jobifai-vm \
  --zone=australia-southeast1-a \
  --machine-type=e2-standard-2 \
  --address=34.87.215.145 \
  --tags=caddy-server \
  --image-family=debian-12 \
  --image-project=debian-cloud \
  --boot-disk-size=100GB \
  --boot-disk-type=pd-balanced

gcloud billing budgets create \
  --billing-account=BILLING_ACCOUNT_ID \
  --display-name="jobifai-alerts" \
  --budget-amount=150USD \
  --threshold-rule=percent=50 \
  --threshold-rule=percent=90 \
  --threshold-rule=percent=100
```

Replace `BILLING_ACCOUNT_ID` from **Billing → Account management**.

## 2. GoDaddy DNS (`jobifai.com.au`)

| Type | Host | Points to |
|------|------|-----------|
| A | `@` | `34.87.215.145` |
| A | `www` | `34.87.215.145` |

Disable GoDaddy forwarding / parking for this domain so A records apply.

## 3. Sign in with Google

Google login is **off** until all three variables are set on the VM and the container is restarted. If any are missing, `/auth/google` is not registered and the button returns **404**.

### Google Cloud Console (project `jobifai-494014`)

1. **APIs & Services → OAuth consent screen** — configure app name, support email. While **Testing**, only **Test users** can sign in (add your Gmail under Test users).
2. **APIs & Services → Credentials → Create credentials → OAuth client ID → Web application**
   - **Authorized redirect URIs** (exact match, no trailing slash):
     - `https://jobifai.com.au/auth/google/callback`
   - Copy **Client ID** and **Client secret**.

### VM `/opt/jobifai/.env`

```bash
GOOGLE_CLIENT_ID=....apps.googleusercontent.com
GOOGLE_CLIENT_SECRET=...
GOOGLE_REDIRECT_URL=https://jobifai.com.au/auth/google/callback
```

Then:

```bash
cd /opt/jobifai && docker compose up -d jobifai
docker compose logs jobifai 2>&1 | tail -20
```

You should see **`Google OAuth enabled`**, not `Google OAuth disabled`.

### Use the apex URL

Open **https://jobifai.com.au** (not `www`). The Caddyfile redirects `www` → apex so OAuth state cookies stay on one host.

### If it still fails

| Symptom | Likely cause |
|---------|----------------|
| 404 on Continue with Google | Missing `GOOGLE_*` in `.env` or container not restarted |
| Google “redirect_uri_mismatch” | Redirect URI in Console ≠ `GOOGLE_REDIRECT_URL` |
| Google “Access blocked” / 403 | Consent screen in Testing and your email not a test user |
| `invalid oauth state` after Google | Mixed `www` / apex (deploy latest Caddyfile) or blocked cookies |
| Blank page / `oauth failed` in URL | Wrong client secret or clock skew on VM |

Redeploy Caddy after Caddyfile changes: `make deploy-gcp-rollout` or copy `Caddyfile` and `docker compose up -d caddy`.

## 4. Build and push image

From repo root on your Mac:

```bash
gcloud auth configure-docker australia-southeast1-docker.pkg.dev

docker build -t australia-southeast1-docker.pkg.dev/jobifai-494014/jobifai/jobifai:latest .
docker push australia-southeast1-docker.pkg.dev/jobifai-494014/jobifai/jobifai:latest
```

Or use Cloud Build:

```bash
gcloud builds submit --tag australia-southeast1-docker.pkg.dev/jobifai-494014/jobifai/jobifai:latest .
```

## 5. VM bootstrap (SSH once)

```bash
gcloud compute ssh jobifai-vm --zone=australia-southeast1-a
```

On the VM:

```bash
sudo apt-get update && sudo apt-get install -y docker.io docker-compose-plugin
sudo usermod -aG docker "$USER"
# log out and back in

sudo mkdir -p /mnt/disks/jobifai/{data,job_applications,resume_style}
sudo mkdir -p /opt/jobifai
sudo chown -R "$USER:$USER" /mnt/disks/jobifai /opt/jobifai
```

Copy to `/opt/jobifai/`:

- `infra/gcp/docker-compose.yml`
- `infra/gcp/Caddyfile`
- `.env` from `infra/gcp/.env.example` (fill `JWT_SECRET`)

```bash
cd /opt/jobifai
docker compose pull
docker compose up -d
docker compose logs -f
```

Caddy obtains Let's Encrypt certs automatically once DNS points at `34.87.215.145`.

## 6. Verify

- https://jobifai.com.au — register / login
- Settings → Launch browser → save Seek/LinkedIn session (noVNC at `/novnc/` when using Docker image)

## 7. Grant admin (production)

There is no env var for admin. `is_admin` lives in SQLite. The user must **sign in once** (Google or email) so a row exists, then on the VM:

```bash
sudo apt-get install -y sqlite3   # once
sqlite3 /mnt/disks/jobifai/data/jobifai.db \
  "UPDATE users SET is_admin = 1 WHERE email = 'you@example.com';
   SELECT email, is_admin FROM users WHERE email = 'you@example.com';"
```

Log out and back in (or hard refresh) so the JWT/session reflects admin. Open **Admin** in the sidebar.

To promote someone else later, use **Admin → Users** in the UI.

## Security notes

- Do **not** expose port **8081** on the firewall; only **443** (and **80** for ACME redirect).
- SSH is currently open to `0.0.0.0/0` in the project — restrict when you can.
- Never commit `.env` or secrets.

## Adding DealFindio later

Use a **separate GCP project** for DealFindio prod if you prefer; or on this VM:

1. Add Postgres/Redis/API/worker services to `docker-compose.yml` (compose profile `dealfindio`).
2. Uncomment API block in `Caddyfile` for `api.dealfindio.com.au`.
3. Point DealFindio DNS at the same static IP (or a second VM).
