# Sub2API Deployment Files

This directory contains files for deploying Sub2API on Linux servers and Apple-silicon Macs.

## Deployment Methods

| Method | Best For | Setup Wizard |
|--------|----------|--------------|
| **Docker Compose** | Quick setup, all-in-one | Not needed (auto-setup) |
| **Apple container** | Native local stack on macOS 26 | Not needed (auto-setup) |
| **Binary Install** | Production servers, systemd | Web-based wizard |

## Files

| File | Description |
|------|-------------|
| `docker-compose.yml` | Docker Compose configuration (named volumes) |
| `docker-compose.local.yml` | Docker Compose configuration (local directories, easy migration) |
| `docker-deploy.sh` | **One-click Docker deployment script (recommended)** |
| `apple-container.sh` | Native Apple `container` lifecycle script |
| `APPLE_CONTAINER.md` | Apple `container` deployment and operations guide |
| `.env.example` | Container environment variables template |
| `DOCKER.md` | Docker Hub documentation |
| `install.sh` | One-click binary installation script |
| `install-datamanagementd.sh` | datamanagementd 一键安装脚本 |
| `sub2api.service` | Systemd service unit file |
| `sub2api-datamanagementd.service` | datamanagementd systemd service unit file |
| `DATAMANAGEMENTD_CN.md` | datamanagementd 部署与联动说明（中文） |
| `config.example.yaml` | Example configuration file |

---

## Apple container Deployment

Apple-silicon Macs running macOS 26 can run the complete Sub2API, PostgreSQL, and Redis stack with Apple `container` 1.1.0 or newer:

```bash
./apple-container.sh init
./apple-container.sh up
./apple-container.sh status
./apple-container.sh logs app -f
```

The script uses Apple named volumes, starts dependencies in order, and performs live readiness checks. It does not provide a continuous restart supervisor; run `./apple-container.sh up` after a host reboot. Docker Compose remains the recommended production deployment path.

See [APPLE_CONTAINER.md](./APPLE_CONTAINER.md) for configuration, upgrades, persistence, networking behavior, and limitations.

---

## Docker Deployment (Recommended)

### Method 1: One-Click Deployment (Recommended)

Use the automated preparation script for the easiest setup:

```bash
# Download and run the preparation script
curl -sSL https://raw.githubusercontent.com/Wei-Shaw/sub2api/main/deploy/docker-deploy.sh | bash

# Or download first, then run
curl -sSL https://raw.githubusercontent.com/Wei-Shaw/sub2api/main/deploy/docker-deploy.sh -o docker-deploy.sh
chmod +x docker-deploy.sh
./docker-deploy.sh
```

**What the script does:**
- Downloads `docker-compose.local.yml` and `.env.example`
- Automatically generates secure secrets for Sub2API and the independent risk-control service
- Creates `.env` file with generated secrets
- Creates necessary data directories (data/, postgres_data/, redis_data/, risk_control_postgres_data/)
- **Displays generated credentials** (POSTGRES_PASSWORD, JWT_SECRET, etc.)

**After running the script:**
```bash
# Start services
docker compose -f docker-compose.local.yml up -d

# View logs
docker compose -f docker-compose.local.yml logs -f sub2api

# If admin password was auto-generated, find it in logs:
docker compose -f docker-compose.local.yml logs sub2api | grep "admin password"

# Access Web UI
# http://localhost:8080
```

### Method 2: Manual Deployment

If you prefer manual control:

```bash
# Clone repository
git clone https://github.com/Wei-Shaw/sub2api.git
cd sub2api/deploy

# Configure environment
cp .env.example .env
chmod 600 .env
	nano .env  # Set required variables, including the risk-control secret and DB password

# Generate secure secrets (recommended)
JWT_SECRET=$(openssl rand -hex 32)
TOTP_ENCRYPTION_KEY=$(openssl rand -hex 32)
RISK_CONTROL_INTERNAL_SECRET=$(openssl rand -hex 32)
RISK_CONTROL_POSTGRES_PASSWORD=$(openssl rand -hex 32)
echo "JWT_SECRET=${JWT_SECRET}" >> .env
echo "TOTP_ENCRYPTION_KEY=${TOTP_ENCRYPTION_KEY}" >> .env
echo "RISK_CONTROL_INTERNAL_SECRET=${RISK_CONTROL_INTERNAL_SECRET}" >> .env
echo "RISK_CONTROL_POSTGRES_PASSWORD=${RISK_CONTROL_POSTGRES_PASSWORD}" >> .env

# Create data directories
mkdir -p data postgres_data redis_data risk_control_postgres_data

# Start all services using local directory version
docker compose -f docker-compose.local.yml up -d

# View logs (check for auto-generated admin password)
docker compose -f docker-compose.local.yml logs -f sub2api

# Access Web UI
# http://localhost:8080
```

### Deployment Version Comparison

| Version | Data Storage | Migration | Best For |
|---------|-------------|-----------|----------|
| **docker-compose.local.yml** | Local directories (./data, ./postgres_data, ./redis_data, ./risk_control_postgres_data) | ✅ Easy (tar entire directory) | Production, need frequent backups/migration |
| **docker-compose.yml** | Named volumes (/var/lib/docker/volumes/, including risk_control_postgres_data) | ⚠️ Requires docker commands | Simple setup, don't need migration |

**Recommendation:** Use `docker-compose.local.yml` (deployed by `docker-deploy.sh`) for easier data management and migration.

### How Auto-Setup Works

When using Docker Compose with `AUTO_SETUP=true`:

1. On first run, the system automatically:
   - Connects to PostgreSQL and Redis
   - Applies database migrations (SQL files in `backend/migrations/*.sql`) and records them in `schema_migrations`
   - Generates JWT secret (if not provided)
   - Creates admin account (password auto-generated if not provided)
   - Writes config.yaml

2. No manual Setup Wizard needed - just configure `.env` and start

3. If `ADMIN_PASSWORD` is not set, check logs for the generated password:
   ```bash
   docker compose logs sub2api | grep "admin password"
   ```

### Risk-control operations

The risk-control service is internal-only. It has no host `ports` mapping; Sub2API reaches it over the Compose network after normal administrator authentication. Keep `RISK_CONTROL_MODE=shadow` for the first rollout and switch to `review` only after confirming real events and reasons in the three user-risk pages.

Before an upgrade, back up its dedicated database:

```bash
docker compose -f docker-compose.local.yml exec -T risk-control-postgres \
  pg_dump -U "$RISK_CONTROL_POSTGRES_USER" -d "$RISK_CONTROL_POSTGRES_DB" -Fc \
  > risk-control-$(date +%Y%m%d-%H%M%S).dump
```

Restore with the service stopped using `pg_restore --clean --if-exists`. The risk service runs its idempotent schema at startup and records schema version `1`; do not delete `risk_control_postgres_data` during rollback. A schema change must include a new versioned migration before production rollout.

### Account-monitor operations

The account monitor runs inside the same `extensions-self` container and stores
facts/aggregates in `risk-control-postgres`. It reads Sub2API only through the
`extensions_self_ro` views with the dedicated `extensions_self_monitor` login.
Do not reuse `POSTGRES_USER` in `ACCOUNT_MONITOR_SOURCE_DATABASE_URL`.

The default is disabled. For production, URL-encode the generated login
password and configure:

```dotenv
ACCOUNT_MONITOR_ENABLED=true
ACCOUNT_MONITOR_SOURCE_DATABASE_URL=postgres://extensions_self_monitor:<URL-encoded-password>@postgres:5432/sub2api?sslmode=disable
ACCOUNT_MONITOR_POLL_SECONDS=60
ACCOUNT_MONITOR_LOOKBACK_SECONDS=300
ACCOUNT_MONITOR_BATCH_SIZE=1000
ACCOUNT_MONITOR_QUERY_TIMEOUT_MS=3000
```

Use only `deploy/ops/publish-custom.sh` for the first enabled release. After
backing up both databases, it runs `install-account-monitor-source.sql`, checks
`SET ROLE extensions_self_monitor_ro`, proves that the login cannot read full
keys or credentials, deploys the verified paired digests, and checks the signed
`data-quality` API. A failed permission probe stops before production mutation.

The publisher also verifies both dump archives, captures the Nginx origin
certificate/key and container/image metadata, and writes exact rollback tags to
`release-metadata.env`. It probes both `usage_source` and `group_dimension`.

The native Vue entries are `/admin/extensions/account-monitor` and
`/admin/extensions/group-monitor`; both use the authenticated admin proxy
`/api/v1/admin/extensions-self/account-monitor/*`. Details and rollback steps are in
[`../docs/ACCOUNT-MONITOR-CHECKLIST.md`](../docs/ACCOUNT-MONITOR-CHECKLIST.md).

After a healthy publish, backfill only the `data-quality` `available_from/to`
interval. The command creates contiguous segments of at most 31 days, waits for
each job, stops on the first failure, and records results in the release backup:

```bash
/root/sub2api/deploy/ops/backfill-account-monitor.sh \
  --from <available-from-RFC3339> --to <available-to-RFC3339> \
  --record-dir /root/backups/sub2api/<release-id>
```

### Custom fork update and release

For the complete Chinese project overview and repeatable release procedure,
read `docs/SUB2API-CUSTOM-OPERATIONS.md`. The section below is the compact
deployment contract.

The only production path for this fork is:

```text
feature -> custom-release -> Custom Release Actions
-> public paired GHCR images -> administrator update button
-> sub2api-release.path -> digest deployment
```

`origin/custom-release` is the only production branch. `custom` is limited to
`upstream/main` forward-compatibility testing; stable custom features may be
selectively `cherry-pick -x` into it, but the entire branch is never merged back.

Actions publishes one immutable pair from the same full SHA:

```text
ghcr.io/listencodes/sub2api-custom:custom-<full-sha>
ghcr.io/listencodes/sub2api-extensions:custom-<full-sha>
```

Documentation-only pushes are the exception: changes limited to Markdown,
`AGENTS.md`, or any `.gitignore` are ignored by the workflow and do not build or
push images. The host classifier compares the full production-to-target diff;
such a target completes the durable job with `docs_only=true` and leaves
production and `release-state.json` unchanged. Any runtime path in the diff
(source, workflow, Dockerfile, Compose, migration, or `deploy/ops/` script)
uses the normal Actions, paired-image, and administrator digest gates.

Production Compose requires anonymous, digest-pinned values:

```dotenv
SUB2API_IMAGE=ghcr.io/listencodes/sub2api-custom@sha256:<digest>
EXTENSIONS_SELF_IMAGE=ghcr.io/listencodes/sub2api-extensions@sha256:<digest>
```

The versioned scripts in `deploy/ops/` are installed on the VPS as follows:

```bash
install -m 0755 deploy/ops/*.sh /opt/sub2api-custom/
install -m 0644 deploy/ops/sub2api-release.path /etc/systemd/system/
install -m 0644 deploy/ops/sub2api-release.service /etc/systemd/system/
systemctl daemon-reload
systemctl enable --now sub2api-release.path
```

The administrator action writes a durable job and `release-trigger` immediately.
The path unit starts the one-shot host orchestrator, which validates only the
latest official stable Release, waits for Actions and both images, rechecks the
branch base, and publishes. There is no release polling consumer or automatic
source update; the independent health-monitor schedule remains.

Before changing source or Compose state, the publisher verifies both database
dumps and backs up Compose, `.env`, Nginx, origin certificates/keys, previous
digests, rollback tags, container/image metadata, and checksums. It deploys
`extensions-self` before `sub2api`. A failed deployment or health gate performs
automatic paired rollback. Database restore is not automatic, and
`risk-control-postgres` is never recreated or replaced.

When a merge conflict occurs, the update status and admin panel list the exact
conflicted files, both commit IDs, the resolution hint, and the diagnostic
artifact path. The snapshot is stored under
`/var/lib/docker/volumes/deploy_sub2api_data/_data/sync-conflicts/<job-id>/`;
production remains unchanged until the conflict is resolved and a new update
is approved.

Implementation, tests, `origin/custom-release` push, Actions/GHCR, backup,
deployment, health, trigger migration, and rollback evidence are reported as
separate results.

### Database Migration Notes (PostgreSQL)

- Migrations are applied in lexicographic order (e.g. `001_...sql`, `002_...sql`).
- `schema_migrations` tracks applied migrations (filename + checksum).
- Migrations are forward-only; rollback requires a DB backup restore or a manual compensating SQL script.

**Verify `users.allowed_groups` → `user_allowed_groups` backfill**

During the incremental GORM→Ent migration, `users.allowed_groups` (legacy `BIGINT[]`) is being replaced by a normalized join table `user_allowed_groups(user_id, group_id)`.

Run this query to compare the legacy data vs the join table:

```sql
WITH old_pairs AS (
  SELECT DISTINCT u.id AS user_id, x.group_id
  FROM users u
  CROSS JOIN LATERAL unnest(u.allowed_groups) AS x(group_id)
  WHERE u.allowed_groups IS NOT NULL
)
SELECT
  (SELECT COUNT(*) FROM old_pairs)           AS old_pair_count,
  (SELECT COUNT(*) FROM user_allowed_groups) AS new_pair_count;
```

### datamanagementd（数据管理）联动

如需启用管理后台“数据管理”功能，请额外部署宿主机 `datamanagementd`：

- 主进程固定探测 `/tmp/sub2api-datamanagement.sock`
- Docker 场景下需把宿主机 Socket 挂载到容器内同路径
- 详细步骤见：`deploy/DATAMANAGEMENTD_CN.md`

### Commands

For **local directory version** (docker-compose.local.yml):

```bash
# Start services
docker compose -f docker-compose.local.yml up -d

# Stop services
docker compose -f docker-compose.local.yml down

# View logs
docker compose -f docker-compose.local.yml logs -f sub2api

# Restart Sub2API only
docker compose -f docker-compose.local.yml restart sub2api

# Update to latest version
docker compose -f docker-compose.local.yml pull
docker compose -f docker-compose.local.yml up -d

# Remove all data (caution!)
docker compose -f docker-compose.local.yml down
rm -rf data/ postgres_data/ redis_data/
```

For **named volumes version** (docker-compose.yml):

```bash
# Start services
docker compose up -d

# Stop services
docker compose down

# View logs
docker compose logs -f sub2api

# Restart Sub2API only
docker compose restart sub2api

# Update to latest version
docker compose pull
docker compose up -d

# Remove all data (caution!)
docker compose down -v
```

### Environment Variables

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `POSTGRES_PASSWORD` | **Yes** | - | PostgreSQL password |
| `JWT_SECRET` | **Recommended** | *(auto-generated)* | JWT secret (fixed for persistent sessions) |
| `TOTP_ENCRYPTION_KEY` | **Recommended** | *(auto-generated)* | TOTP encryption key (fixed for persistent 2FA) |
| `SERVER_PORT` | No | `8080` | Server port |
| `ADMIN_EMAIL` | No | `admin@sub2api.local` | Admin email |
| `ADMIN_PASSWORD` | No | *(auto-generated)* | Admin password |
| `ACCOUNT_MONITOR_ENABLED` | No | `false` | Enable account-monitor collection and admin page |
| `ACCOUNT_MONITOR_SOURCE_DATABASE_URL` | When enabled | - | Dedicated `extensions_self_monitor` read-only DSN |
| `ACCOUNT_MONITOR_POLL_SECONDS` | No | `60` | Incremental collection interval |
| `ACCOUNT_MONITOR_LOOKBACK_SECONDS` | No | `300` | Late-event lookback window |
| `ACCOUNT_MONITOR_BATCH_SIZE` | No | `1000` | Maximum source rows per page |
| `ACCOUNT_MONITOR_QUERY_TIMEOUT_MS` | No | `3000` | Safe-view query timeout |
| `TZ` | No | `Asia/Shanghai` | Timezone |
| `GEMINI_OAUTH_CLIENT_ID` | No | *(builtin)* | Google OAuth client ID (Gemini OAuth). Leave empty to use the built-in Gemini CLI client. |
| `GEMINI_OAUTH_CLIENT_SECRET` | No | *(builtin)* | Google OAuth client secret (Gemini OAuth). Leave empty to use the built-in Gemini CLI client. |
| `GEMINI_OAUTH_SCOPES` | No | *(default)* | OAuth scopes (Gemini OAuth) |
| `GEMINI_QUOTA_POLICY` | No | *(empty)* | JSON overrides for Gemini local quota simulation (Code Assist only). |

See `.env.example` for all available options.

> **Note:** The `docker-deploy.sh` script automatically generates `JWT_SECRET`, `TOTP_ENCRYPTION_KEY`, and `POSTGRES_PASSWORD` for you.

### Easy Migration (Local Directory Version)

When using `docker-compose.local.yml`, all data is stored in local directories, making migration simple:

```bash
# On source server: Stop services and create archive
cd /path/to/deployment
docker compose -f docker-compose.local.yml down
cd ..
tar czf sub2api-complete.tar.gz deployment/

# Transfer to new server
scp sub2api-complete.tar.gz user@new-server:/path/to/destination/

# On new server: Extract and start
tar xzf sub2api-complete.tar.gz
cd deployment/
docker compose -f docker-compose.local.yml up -d
```

Your entire deployment (configuration + data) is migrated!

---

## Gemini OAuth Configuration

Sub2API supports three methods to connect to Gemini:

### Method 1: Code Assist OAuth (Recommended for GCP Users)

**No configuration needed** - always uses the built-in Gemini CLI OAuth client (public).

1. Leave `GEMINI_OAUTH_CLIENT_ID` and `GEMINI_OAUTH_CLIENT_SECRET` empty
2. In the Admin UI, create a Gemini OAuth account and select **"Code Assist"** type
3. Complete the OAuth flow in your browser

> Note: Even if you configure `GEMINI_OAUTH_CLIENT_ID` / `GEMINI_OAUTH_CLIENT_SECRET` for AI Studio OAuth,
> Code Assist OAuth will still use the built-in Gemini CLI client.

**Requirements:**
- Google account with access to Google Cloud Platform
- A GCP project (auto-detected or manually specified)

**How to get Project ID (if auto-detection fails):**
1. Go to [Google Cloud Console](https://console.cloud.google.com/)
2. Click the project dropdown at the top of the page
3. Copy the Project ID (not the project name) from the list
4. Common formats: `my-project-123456` or `cloud-ai-companion-xxxxx`

### Method 2: AI Studio OAuth (For Regular Google Accounts)

Requires your own OAuth client credentials.

**Step 1: Create OAuth Client in Google Cloud Console**

1. Go to [Google Cloud Console - Credentials](https://console.cloud.google.com/apis/credentials)
2. Create a new project or select an existing one
3. **Enable the Generative Language API:**
   - Go to "APIs & Services" → "Library"
   - Search for "Generative Language API"
   - Click "Enable"
4. **Configure OAuth Consent Screen** (if not done):
   - Go to "APIs & Services" → "OAuth consent screen"
   - Choose "External" user type
   - Fill in app name, user support email, developer contact
   - Add scopes: `https://www.googleapis.com/auth/generative-language.retriever` (and optionally `https://www.googleapis.com/auth/cloud-platform`)
   - Add test users (your Google account email)
5. **Create OAuth 2.0 credentials:**
   - Go to "APIs & Services" → "Credentials"
   - Click "Create Credentials" → "OAuth client ID"
   - Application type: **Web application** (or **Desktop app**)
   - Name: e.g., "Sub2API Gemini"
   - Authorized redirect URIs: Add `http://localhost:1455/auth/callback`
6. Copy the **Client ID** and **Client Secret**
7. **⚠️ Publish to Production (IMPORTANT):**
   - Go to "APIs & Services" → "OAuth consent screen"
   - Click "PUBLISH APP" to move from Testing to Production
   - **Testing mode limitations:**
     - Only manually added test users can authenticate (max 100 users)
     - Refresh tokens expire after 7 days
     - Users must be re-added periodically
   - **Production mode:** Any Google user can authenticate, tokens don't expire
   - Note: For sensitive scopes, Google may require verification (demo video, privacy policy)

**Step 2: Configure Environment Variables**

```bash
GEMINI_OAUTH_CLIENT_ID=your-client-id.apps.googleusercontent.com
GEMINI_OAUTH_CLIENT_SECRET=GOCSPX-your-client-secret

# 可选：如需使用 Gemini CLI 内置 OAuth Client（Code Assist / Google One）
# 安全说明：本仓库不会内置该 client_secret，请在运行环境通过环境变量注入。
# GEMINI_CLI_OAUTH_CLIENT_SECRET=GOCSPX-your-built-in-secret
```

**Step 3: Create Account in Admin UI**

1. Create a Gemini OAuth account and select **"AI Studio"** type
2. Complete the OAuth flow
   - After consent, your browser will be redirected to `http://localhost:1455/auth/callback?code=...&state=...`
   - Copy the full callback URL (recommended) or just the `code` and paste it back into the Admin UI

### Method 3: API Key (Simplest)

1. Go to [Google AI Studio](https://aistudio.google.com/app/apikey)
2. Click "Create API key"
3. In Admin UI, create a Gemini **API Key** account
4. Paste your API key (starts with `AIza...`)

### Comparison Table

| Feature | Code Assist OAuth | AI Studio OAuth | API Key |
|---------|-------------------|-----------------|---------|
| Setup Complexity | Easy (no config) | Medium (OAuth client) | Easy |
| GCP Project Required | Yes | No | No |
| Custom OAuth Client | No (built-in) | Yes (required) | N/A |
| Rate Limits | GCP quota | Standard | Standard |
| Best For | GCP developers | Regular users needing OAuth | Quick testing |

---

## Binary Installation

For production servers using systemd.

### One-Line Installation

```bash
curl -sSL https://raw.githubusercontent.com/Wei-Shaw/sub2api/main/deploy/install.sh | sudo bash
```

### Manual Installation

1. Download the latest release from [GitHub Releases](https://github.com/Wei-Shaw/sub2api/releases)
2. Extract and copy the binary to `/opt/sub2api/`
3. Copy `sub2api.service` to `/etc/systemd/system/`
4. Run:
   ```bash
   sudo systemctl daemon-reload
   sudo systemctl enable sub2api
   sudo systemctl start sub2api
   ```
5. Open the Setup Wizard in your browser to complete configuration

### Commands

```bash
# Install
sudo ./install.sh

# Upgrade
sudo ./install.sh upgrade

# Uninstall
sudo ./install.sh uninstall
```

### Service Management

```bash
# Start the service
sudo systemctl start sub2api

# Stop the service
sudo systemctl stop sub2api

# Restart the service
sudo systemctl restart sub2api

# Check status
sudo systemctl status sub2api

# View logs
sudo journalctl -u sub2api -f

# Enable auto-start on boot
sudo systemctl enable sub2api
```

### Configuration

#### Server Address and Port

During installation, you will be prompted to configure the server listen address and port. These settings are stored in the systemd service file as environment variables.

To change after installation:

1. Edit the systemd service:
   ```bash
   sudo systemctl edit sub2api
   ```

2. Add or modify:
   ```ini
   [Service]
   Environment=SERVER_HOST=0.0.0.0
   Environment=SERVER_PORT=3000
   ```

3. Reload and restart:
   ```bash
   sudo systemctl daemon-reload
   sudo systemctl restart sub2api
   ```

#### Gemini OAuth Configuration

If you need to use AI Studio OAuth for Gemini accounts, add the OAuth client credentials to the systemd service file:

1. Edit the service file:
   ```bash
   sudo nano /etc/systemd/system/sub2api.service
   ```

2. Add your OAuth credentials in the `[Service]` section (after the existing `Environment=` lines):
   ```ini
   Environment=GEMINI_OAUTH_CLIENT_ID=your-client-id.apps.googleusercontent.com
   Environment=GEMINI_OAUTH_CLIENT_SECRET=GOCSPX-your-client-secret
   ```

   如需使用“内置 Gemini CLI OAuth Client”（Code Assist / Google One），还需要注入：
   ```ini
   Environment=GEMINI_CLI_OAUTH_CLIENT_SECRET=GOCSPX-your-built-in-secret
   ```

3. Reload and restart:
   ```bash
   sudo systemctl daemon-reload
   sudo systemctl restart sub2api
   ```

> **Note:** Code Assist OAuth does not require any configuration - it uses the built-in Gemini CLI client.
> See the [Gemini OAuth Configuration](#gemini-oauth-configuration) section above for detailed setup instructions.

#### Application Configuration

The main config file is at `/etc/sub2api/config.yaml` (created by Setup Wizard).

### Prerequisites

- Linux server (Ubuntu 20.04+, Debian 11+, CentOS 8+, etc.)
- PostgreSQL 14+
- Redis 6+
- systemd

### Directory Structure

```
/opt/sub2api/
├── sub2api              # Main binary
├── sub2api.backup       # Backup (after upgrade)
└── data/                # Runtime data

/etc/sub2api/
└── config.yaml          # Configuration file
```

---

## Troubleshooting

### Docker

For **local directory version**:

```bash
# Check container status
docker compose -f docker-compose.local.yml ps

# View detailed logs
docker compose -f docker-compose.local.yml logs --tail=100 sub2api

# Check database connection
docker compose -f docker-compose.local.yml exec postgres pg_isready

# Check Redis connection
docker compose -f docker-compose.local.yml exec redis redis-cli ping

# Restart all services
docker compose -f docker-compose.local.yml restart

# Check data directories
ls -la data/ postgres_data/ redis_data/
```

For **named volumes version**:

```bash
# Check container status
docker compose ps

# View detailed logs
docker compose logs --tail=100 sub2api

# Check database connection
docker compose exec postgres pg_isready

# Check Redis connection
docker compose exec redis redis-cli ping

# Restart all services
docker compose restart
```

### Binary Install

```bash
# Check service status
sudo systemctl status sub2api

# View recent logs
sudo journalctl -u sub2api -n 50

# Check config file
sudo cat /etc/sub2api/config.yaml

# Check PostgreSQL
sudo systemctl status postgresql

# Check Redis
sudo systemctl status redis
```

### Common Issues

1. **Port already in use**: Change `SERVER_PORT` in `.env` or systemd config
2. **Database connection failed**: Check PostgreSQL is running and credentials are correct
3. **Redis connection failed**: Check Redis is running and password is correct
4. **Permission denied**: Ensure proper file ownership for binary install
5. **Account monitor unavailable**: Check `extensions-self` logs, `data-quality`, source-role grants, and source DSN; do not interpret an incomplete historical range as zero traffic

---

## TLS Fingerprint Configuration

Sub2API supports TLS fingerprint simulation to make requests appear as if they come from the official Claude CLI (Node.js client).

> **💡 Tip:** Visit **[tls.sub2api.org](https://tls.sub2api.org/)** to get TLS fingerprint information for different devices and browsers.

### Default Behavior

- Built-in `claude_cli_v2` profile simulates Node.js 20.x + OpenSSL 3.x
- JA3 Hash: `1a28e69016765d92e3b381168d68922c`
- JA4: `t13d5911h1_a33745022dd6_1f22a2ca17c4`
- Profile selection: `accountID % profileCount`

### Configuration

```yaml
gateway:
  tls_fingerprint:
    enabled: true  # Global switch
    profiles:
      # Simple profile (uses default cipher suites)
      profile_1:
        name: "Profile 1"

      # Profile with custom cipher suites (use compact array format)
      profile_2:
        name: "Profile 2"
        cipher_suites: [4866, 4867, 4865, 49199, 49195, 49200, 49196]
        curves: [29, 23, 24]
        point_formats: 0

      # Another custom profile
      profile_3:
        name: "Profile 3"
        cipher_suites: [4865, 4866, 4867, 49199, 49200]
        curves: [29, 23, 24, 25]
```

### Profile Fields

| Field | Type | Description |
|-------|------|-------------|
| `name` | string | Display name (required) |
| `cipher_suites` | []uint16 | Cipher suites in decimal. Empty = default |
| `curves` | []uint16 | Elliptic curves in decimal. Empty = default |
| `point_formats` | []uint8 | EC point formats. Empty = default |

### Common Values Reference

**Cipher Suites (TLS 1.3):** `4865` (AES_128_GCM), `4866` (AES_256_GCM), `4867` (CHACHA20)

**Cipher Suites (TLS 1.2):** `49195`, `49196`, `49199`, `49200` (ECDHE variants)

**Curves:** `29` (X25519), `23` (P-256), `24` (P-384), `25` (P-521)
