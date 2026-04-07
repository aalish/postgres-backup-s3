# PostgreSQL Backup to S3

This repository now contains two production-oriented Go binaries:

- `pgbackup`: a CLI that creates a full PostgreSQL backup with `pg_dump`, validates the archive with `pg_restore --list`, uploads it to Amazon S3, verifies the uploaded object, and only then removes the local dump file.
- `pgbackupd`: a secure web dashboard for triggering backups, browsing S3 backup history, editing runtime settings stored in MongoDB Atlas, and enforcing retention automatically.

The generated dump is a PostgreSQL custom archive (`.dump`) created with `pg_dump --format=custom --create --blobs`, so it contains the schema, data, indexes, constraints, triggers, functions, and other database objects needed for a complete database restore.

## Features

- Full PostgreSQL backups using `pg_dump`
- Timestamped, compressed custom-format archives
- Configurable database connection details
- S3 uploads using either static AWS keys or IAM role based credentials
- Structured logging in JSON or text format
- Upload retries with exponential backoff
- Archive validation before upload
- Upload verification before deleting the local file
- CLI-friendly exit codes for cron, systemd, or CI/CD jobs
- Secure admin dashboard with JWT cookie authentication and CSRF protection
- MongoDB Atlas-backed runtime settings with defaults
- Automated retention enforcement with audit and retention-run logs

## Project Layout

```text
cmd/pgbackup/           CLI entrypoint
cmd/pgbackupd/          dashboard server entrypoint
internal/config/        environment and env-file configuration loading
internal/backup/        pg_dump execution and archive validation
internal/storage/       S3 upload and verification
internal/retry/         retry helper
internal/service/       orchestration of the backup workflow
internal/dashboard/     embedded admin UI, auth, and HTTP handlers
internal/store/         MongoDB persistence for settings, sessions, and logs
internal/runtimecfg/    mutable runtime settings loaded from MongoDB
```

## Requirements

- Go 1.25+
- PostgreSQL client tools installed on the host:
  - `pg_dump`
  - `pg_restore`
- AWS credentials available through either:
  - `AWS_ACCESS_KEY_ID` and `AWS_SECRET_ACCESS_KEY`
  - an IAM role / default AWS credential chain
- MongoDB Atlas reachable from the host when running `pgbackupd`
- Network access from the host to PostgreSQL and Amazon S3

## Setup

1. Copy the example configuration:

   ```bash
   cp .env.example pgbackup.env
   ```

2. Edit `pgbackup.env` with your PostgreSQL and S3 values.

3. Build the binary:

   ```bash
   go build -o bin/pgbackup ./cmd/pgbackup
   ```

4. Run a backup:

   ```bash
   ./bin/pgbackup -config ./pgbackup.env
   ```

Environment variables already present in the shell take precedence over values inside the env file, which is useful for secret injection in production.

## Dashboard Setup

The dashboard server reuses the same PostgreSQL and AWS connection settings as the CLI, then adds:

- `ADMIN_USERNAME`
- `ADMIN_PASSWORD`
- `JWT_SECRET`
- `MONGODB_URI`

Build and run the dashboard:

```bash
go build -o bin/pgbackupd ./cmd/pgbackupd
./bin/pgbackupd -config ./pgbackup.env
```

By default the dashboard listens on `:8080`. For local HTTP development, set `COOKIE_SECURE=false`; keep it `true` in production behind HTTPS.

## GitHub Release Automation

This repository includes a GitHub Actions workflow at [.github/workflows/release.yml](/Users/xero/data/Neelgai/postgres-backup/.github/workflows/release.yml) that:

- runs on `ubuntu-22.04`
- runs `go test ./...`
- builds Linux `amd64` binaries with the Git tag embedded in `pgbackup -version` and `pgbackupd -version`
- uploads raw binaries as `pgbackup_<tag>_linux_amd64` and `pgbackupd_<tag>_linux_amd64`
- packages both binaries as `.tar.gz` archives
- uploads a `.sha256` checksum file for each binary and archive to the GitHub Release page

To publish a release asset:

```bash
git tag v1.0.0
git push origin v1.0.0
```

The workflow is tag-driven and triggers on tags matching `v*`.

## Release Requirements

For the release workflow to succeed on GitHub, you need:

- Actions enabled for the repository
- the default `GITHUB_TOKEN` allowed to create and update releases
- a pushed Git tag such as `v1.0.0`

The downloaded binaries are built for Linux `amd64` and are a good fit for Ubuntu 22.04 x86_64 hosts.

## Runtime Requirements For Released Binary

The GitHub Release assets contain `pgbackup` and `pgbackupd`. The target Ubuntu 22.04 host still needs:

- `pg_dump`
- `pg_restore`
- network access to PostgreSQL and S3
- your runtime configuration file or environment variables
- MongoDB Atlas access for the dashboard server

Example Ubuntu package install:

```bash
sudo apt-get update
sudo apt-get install -y postgresql-client
```

If you need a specific PostgreSQL client version for compatibility with your server, install that version explicitly instead of the generic package.

## Dashboard Capabilities

`pgbackupd` provides:

- a secure login page using short-lived JWT access cookies plus refresh-token rotation
- HttpOnly, Secure, `SameSite=Strict` cookies for auth tokens
- CSRF protection on state-changing endpoints
- login rate limiting
- a paginated backup table sourced from S3
- manual backup triggering with live status polling
- MongoDB-backed settings for retention, schedule, S3 bucket/prefix, output path, compression, and webhook notifications
- automatic retention runs on startup, config changes, and a background hourly loop
- audit logs for auth, backup, retention, and config changes

## Configuration

The binary reads configuration from environment variables, optionally seeded from a `KEY=VALUE` file passed through `-config`.

| Variable | Required | Default | Description |
| --- | --- | --- | --- |
| `PGHOST` | Yes |  | PostgreSQL host |
| `PGPORT` | No | `5432` | PostgreSQL port |
| `PGUSER` | Yes |  | PostgreSQL username |
| `PGPASSWORD` | No |  | PostgreSQL password. Can be empty if `.pgpass` or another libpq auth method is used |
| `PGDATABASE` | Yes |  | Database name to back up |
| `PG_DUMP_PATH` | No | `pg_dump` | Path to the `pg_dump` binary |
| `PG_RESTORE_PATH` | No | `pg_restore` | Path to the `pg_restore` binary used for validation |
| `BACKUP_OUTPUT_DIR` | No | `./backups` | Temporary local directory for the dump before upload |
| `BACKUP_FILENAME_PREFIX` | No | `PGDATABASE` | Prefix for generated dump filenames |
| `BACKUP_COMPRESSION` | No | `6` | `pg_dump` compression level from `0` to `9` |
| `AWS_REGION` | Yes |  | AWS region for S3 |
| `S3_BUCKET` | Yes |  | Destination S3 bucket |
| `S3_PREFIX` | No | empty | Prefix/folder inside the bucket |
| `AWS_ACCESS_KEY_ID` | No | empty | Static AWS access key |
| `AWS_SECRET_ACCESS_KEY` | No | empty | Static AWS secret key |
| `AWS_SESSION_TOKEN` | No | empty | Optional session token for temporary credentials |
| `S3_USE_PATH_STYLE` | No | `false` | Optional S3 path-style access toggle |
| `S3_ENDPOINT_URL` | No | empty | Optional custom S3 endpoint |
| `UPLOAD_MAX_ATTEMPTS` | No | `5` | Number of upload attempts |
| `UPLOAD_INITIAL_DELAY` | No | `2s` | Initial delay between retries |
| `UPLOAD_MAX_DELAY` | No | `30s` | Maximum backoff delay |
| `LOG_LEVEL` | No | `info` | `debug`, `info`, `warn`, or `error` |
| `LOG_FORMAT` | No | `json` | `json` or `text` |

## Usage Examples

### Run once with an env file

```bash
./bin/pgbackup -config /etc/pgbackup/pgbackup.env
```

### Run once with shell environment variables

```bash
export PGHOST=db.internal
export PGPORT=5432
export PGUSER=backup_user
export PGPASSWORD='super-secret'
export PGDATABASE=appdb
export AWS_REGION=us-east-1
export S3_BUCKET=company-postgres-backups
export S3_PREFIX=prod/appdb

./bin/pgbackup
```

### Run with verbose progress logs

```bash
LOG_LEVEL=debug LOG_FORMAT=text ./bin/pgbackup -config /etc/pgbackup/pgbackup.env
```

### Schedule with cron

```cron
0 2 * * * /opt/pgbackup/bin/pgbackup -config /etc/pgbackup/pgbackup.env >> /var/log/pgbackup.log 2>&1
```

## Backup Flow

1. Load configuration from environment variables and optional env file.
2. Run `pg_dump` in custom archive mode with compression and `--create`.
3. Confirm that the dump file exists and is non-empty.
4. Run `pg_restore --list` against the archive to validate that it is readable.
5. Upload the archive to S3.
6. Verify the uploaded object using `HeadObject` and compare its size with the local dump.
7. Delete the local dump file only after successful upload verification.

If any step fails, the command exits with a non-zero status code and the local dump file is left in place for investigation.

## Logging and Progress

- `LOG_LEVEL=info` shows major workflow progress such as dump creation, validation, upload, verification, and cleanup.
- `LOG_LEVEL=debug` adds sanitized configuration details, command paths, and retry timing details.
- `LOG_FORMAT=text` is usually easier to read interactively in a terminal.
- `LOG_FORMAT=json` is better for log aggregation systems.

Example:

```bash
LOG_LEVEL=debug LOG_FORMAT=text ./bin/pgbackup -config ./pgbackup.env
```

## Restore Flow

Because the archive is created with `--create`, you can restore the database from the dump alone.

Example restore command:

```bash
export PGPASSWORD='restore-password'
pg_restore \
  --clean \
  --if-exists \
  --create \
  --dbname=postgres \
  /path/to/appdb-prod_20260316T020000Z.dump
```

Notes:

- Run the restore as a PostgreSQL superuser or another role with permission to create databases and owned objects.
- The target roles referenced inside the dump should already exist if ownership and grants must be restored exactly.
- PostgreSQL global objects such as cluster roles and tablespaces are not included in a single-database `pg_dump`; back them up separately with `pg_dumpall --globals-only` if your environment depends on them.

## Deployment Notes

- Install the binary on a host that has `pg_dump` and `pg_restore` from a version compatible with your PostgreSQL server.
- Store configuration in a root-readable file such as `/etc/pgbackup/pgbackup.env` or inject values through a secret manager.
- Prefer IAM roles over long-lived AWS access keys when running on EC2, ECS, or EKS.
- Ensure the S3 bucket has versioning and server-side encryption enabled.
- Limit the PostgreSQL user to the permissions needed to read the target database.
- Use OS-level process supervision like `systemd`, Kubernetes CronJobs, or a managed scheduler for recurring execution.

## Exit Codes

- `0`: backup completed successfully
- `1`: runtime failure during backup, validation, upload, or cleanup
- `2`: configuration or startup failure

## Design Choices

- **Go CLI**: produces a single deployable binary, has strong standard-library support for process execution and logging, and works well for scheduled jobs.
- **PostgreSQL custom archive format**: supports compression, `pg_restore`, and complete database restores while staying portable.
- **`pg_restore --list` validation**: confirms the dump archive is structurally readable before any upload is attempted.
- **S3 `HeadObject` verification**: ensures the uploaded object exists and matches the local file size before cleanup.
- **Environment-based configuration**: keeps the service easy to deploy in containers, VMs, and schedulers without baking secrets into the binary.

## Assumptions

- `pg_dump` and `pg_restore` are installed and available on the runtime host.
- The PostgreSQL user can connect and read all objects that need to be backed up.
- The S3 bucket already exists and the provided AWS identity can write to it.
- Backing up database-global objects such as roles is handled separately if required.

## Security Considerations

- Avoid committing real credentials to source control. Use the env example only as a template.
- Prefer IAM roles or short-lived AWS credentials over long-lived static keys.
- Restrict file permissions on configuration files because they may contain database passwords.
- Consider enabling server-side encryption, bucket versioning, object lock, and lifecycle policies on the S3 bucket.
- Ensure temporary backup storage is on encrypted disk if local-at-rest protection is required.
