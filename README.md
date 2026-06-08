# PostgreSQL Multi-Database Backup & Restore System

A production-grade PostgreSQL backup solution with multi-database support, automated restore capabilities, role-based access control, and a secure web dashboard for managing database backups across multiple PostgreSQL instances.

## 🌟 Key Features

### Core Capabilities
- **🔄 Multi-Database Support**: Backup multiple PostgreSQL databases with individual configurations
- **⏮️ Database Restore**: Restore databases from S3 or local backups with comprehensive options
- **👥 Multi-User Authentication**: Role-based access control with admin, operator, and viewer roles
- **🌐 Web Dashboard**: Secure web interface for backup management and monitoring
- **☁️ S3 Integration**: Automatic upload to AWS S3 with verification
- **🔒 Security First**: JWT authentication, CSRF protection, encrypted passwords
- **📊 Audit Logging**: Complete audit trail for compliance
- **⚡ Parallel Execution**: Backup multiple databases concurrently
- **📅 Scheduled Backups**: Cron-based scheduling per database
- **♻️ Retention Management**: Automatic cleanup of old backups

### Advanced Features
- **Selective Restore**: Restore specific schemas, tables, or data-only
- **Dry-Run Mode**: Test restore operations without making changes
- **Compression Control**: Per-database compression levels (0-9)
- **SSL/TLS Support**: Secure connections to PostgreSQL servers
- **Custom S3 Endpoints**: Support for S3-compatible storage
- **Webhook Notifications**: Alert on backup/restore failures
- **Database Health Monitoring**: Track backup status and statistics

## 📦 Components

### 1. **pgbackup** - Multi-Database Backup CLI
Handles automated backups for single or multiple PostgreSQL databases with validation and S3 upload.

### 2. **pgrestore** - Database Restore CLI
Comprehensive restore tool with safety checks, selective restore, and progress tracking.

### 3. **pgbackupd** - Web Dashboard
Secure web interface for backup management, user administration, and system monitoring.

## 🚀 Quick Start

### Prerequisites
- Go 1.25+ (for building from source)
- PostgreSQL client tools (`pg_dump`, `pg_restore`)
- AWS S3 bucket or S3-compatible storage
- MongoDB (for dashboard only)
- Network access to PostgreSQL servers

### Installation

#### Option 1: Download Pre-built Binaries
```bash
# Download latest release
wget https://github.com/neelgai/postgres-backup/releases/latest/download/pgbackup_linux_amd64
wget https://github.com/neelgai/postgres-backup/releases/latest/download/pgrestore_linux_amd64
wget https://github.com/neelgai/postgres-backup/releases/latest/download/pgbackupd_linux_amd64

# Make executable
chmod +x pgbackup_linux_amd64 pgrestore_linux_amd64 pgbackupd_linux_amd64

# Move to PATH
sudo mv pgbackup_linux_amd64 /usr/local/bin/pgbackup
sudo mv pgrestore_linux_amd64 /usr/local/bin/pgrestore
sudo mv pgbackupd_linux_amd64 /usr/local/bin/pgbackupd
```

#### Option 2: Build from Source
```bash
# Clone repository
git clone https://github.com/neelgai/postgres-backup.git
cd postgres-backup

# Build all binaries
go build -o bin/pgbackup ./cmd/pgbackup
go build -o bin/pgrestore ./cmd/pgrestore
go build -o bin/pgbackupd ./cmd/pgbackupd
```

### Basic Configuration

#### Single Database Mode
```bash
# Copy example configuration
cp .env.example .env

# Edit with your settings
vim .env

# Run backup
./bin/pgbackup -config .env
```

#### Multi-Database Mode
```bash
# Create database configuration
cp databases.json.example databases.json
vim databases.json

# Set environment variables
export MULTI_DATABASE_MODE=true
export DATABASE_CONFIG_FILE=databases.json

# Run backup for all databases
./bin/pgbackup -config .env -parallel
```

## 🔧 Configuration

### Environment Variables

#### Core Configuration
| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `MULTI_DATABASE_MODE` | No | `false` | Enable multi-database support |
| `DATABASE_CONFIG_FILE` | No | `databases.json` | Path to database configuration file |
| `PGHOST` | Yes* | | PostgreSQL host (single-db mode) |
| `PGPORT` | No | `5432` | PostgreSQL port |
| `PGUSER` | Yes* | | PostgreSQL username |
| `PGPASSWORD` | No | | PostgreSQL password |
| `PGDATABASE` | Yes* | | Database name |

*Required only in single-database mode

#### S3 Configuration
| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `AWS_REGION` | Yes | | AWS region |
| `S3_BUCKET` | Yes | | S3 bucket name |
| `S3_PREFIX` | No | | S3 key prefix/folder |
| `AWS_ACCESS_KEY_ID` | No | | AWS access key |
| `AWS_SECRET_ACCESS_KEY` | No | | AWS secret key |
| `S3_ENDPOINT_URL` | No | | Custom S3 endpoint |

#### Dashboard Configuration
| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `HTTP_ADDR` | No | `:8080` | Web server address |
| `ADMIN_USERNAME` | Yes | | Initial admin username |
| `ADMIN_PASSWORD` | Yes | | Initial admin password |
| `JWT_SECRET` | Yes | | JWT signing secret (min 32 chars) |
| `MONGODB_URI` | Yes | | MongoDB connection string |
| `MONGODB_DATABASE` | No | `pgbackup` | MongoDB database name |

### Multi-Database Configuration File

Create a `databases.json` file:

```json
{
  "databases": [
    {
      "id": "production_main",
      "name": "Production Main Database",
      "host": "prod-db.example.com",
      "port": 5432,
      "user": "backup_user",
      "database": "main_app",
      "ssl_mode": "require",
      "enabled": true,
      "backup_schedule": "0 2 * * *",
      "retention_days": 30,
      "compression_level": 6,
      "s3_prefix": "backups/production/main",
      "filename_prefix": "prod_main",
      "tags": ["critical", "production"]
    },
    {
      "id": "analytics",
      "name": "Analytics Database",
      "host": "analytics-db.example.com",
      "port": 5432,
      "user": "analytics_user",
      "database": "analytics",
      "ssl_mode": "verify-full",
      "enabled": true,
      "backup_schedule": "0 23 * * *",
      "retention_days": 90,
      "compression_level": 9,
      "s3_prefix": "backups/analytics",
      "filename_prefix": "analytics",
      "tags": ["analytics", "data-warehouse"]
    }
  ]
}
```

### Database-Specific Passwords

Set passwords via environment variables:

```bash
# General fallback password
export PGPASSWORD=default-password

# Database-specific passwords
export PGPASSWORD_PRODUCTION_MAIN=prod-main-password
export PGPASSWORD_ANALYTICS=analytics-password
```

## 📚 Usage Examples

### Backup Operations

#### Backup All Databases
```bash
# Sequential execution
pgbackup -config .env

# Parallel execution (faster)
pgbackup -config .env -parallel

# Verbose output
LOG_LEVEL=debug LOG_FORMAT=text pgbackup -config .env
```

#### Backup Specific Database
```bash
# Backup only production database
pgbackup -config .env -database production_main

# Backup only analytics
pgbackup -config .env -database analytics
```

#### Schedule with Cron
```cron
# Backup all databases daily at 2 AM
0 2 * * * /usr/local/bin/pgbackup -config /etc/pgbackup/.env -parallel

# Backup specific database
0 3 * * * /usr/local/bin/pgbackup -config /etc/pgbackup/.env -database analytics
```

### Restore Operations

#### List Available Backups
```bash
# List all backups
pgrestore -config .env --list

# List backups for specific database
pgrestore -config .env --list --database-id production_main

# List with details
pgrestore -config .env --list --verbose
```

#### Basic Restore
```bash
# Restore from S3
pgrestore -config .env \
  --s3 s3://my-bucket/backups/prod_main_20240101_020000.dump \
  --target-db restored_production

# Restore from local file
pgrestore -config .env \
  --local /path/to/backup.dump \
  --target-db my_restored_db
```

#### Advanced Restore Options
```bash
# Clean restore (drop existing objects)
pgrestore -config .env \
  --s3 s3://my-bucket/backup.dump \
  --clean --create-db \
  --target-db new_database

# Restore specific schemas only
pgrestore -config .env \
  --local backup.dump \
  --schemas public,app_schema \
  --target-db restored_db

# Data-only restore (no schema)
pgrestore -config .env \
  --local backup.dump \
  --data-only \
  --target-db existing_db

# Parallel restore with 4 jobs
pgrestore -config .env \
  --local large_backup.dump \
  --jobs 4 \
  --target-db restored_db

# Dry run (test without making changes)
pgrestore -config .env \
  --local backup.dump \
  --dry-run --verbose
```

#### Restore with Safety Options
```bash
# Skip confirmation prompt
pgrestore -config .env \
  --s3 s3://bucket/backup.dump \
  --yes \
  --target-db restored_db

# Force restore despite warnings
pgrestore -config .env \
  --local backup.dump \
  --force \
  --target-db restored_db

# Keep downloaded S3 file
pgrestore -config .env \
  --s3 s3://bucket/backup.dump \
  --keep-download \
  --target-db restored_db
```

### Dashboard Operations

#### Start Dashboard Server
```bash
# Start with configuration file
pgbackupd -config .env

# Start with environment variables
MONGODB_URI=mongodb://localhost:27017 \
ADMIN_USERNAME=admin \
ADMIN_PASSWORD=secure-password \
JWT_SECRET=random-32-character-string-minimum \
pgbackupd
```

#### Access Dashboard
```
http://localhost:8080
```

Default credentials (first run):
- Username: `admin`
- Password: `admin` (change immediately)

## 👥 User Management

### User Roles and Permissions

| Role | Description | Permissions |
|------|-------------|------------|
| **Admin** | Full system access | All operations including user management |
| **Operator** | Backup/restore operations | Create backups/restores, view settings |
| **Viewer** | Read-only access | View backups, databases, and logs |

### Managing Users

#### Via Environment Variables (Initial Setup)
```bash
# Set initial admin credentials
export ADMIN_USERNAME=admin
export ADMIN_PASSWORD=secure-admin-password
```

#### Via MongoDB (After Setup)
The system automatically creates user management collections. Additional users can be added through:
1. Web dashboard UI (when fully implemented)
2. Direct MongoDB operations
3. API endpoints (when implemented)

### Database Access Control

Users can be restricted to specific databases:

```json
{
  "username": "john.doe",
  "role": "operator",
  "allowed_databases": ["production_main", "staging"],
  "permissions": {
    "view_backups": true,
    "create_backups": true,
    "create_restores": true,
    "view_settings": true
  }
}
```

## 🔒 Security Best Practices

### 1. PostgreSQL Security
```sql
-- Create dedicated backup user
CREATE USER backup_user WITH PASSWORD 'secure-password';
GRANT CONNECT ON DATABASE myapp TO backup_user;
GRANT USAGE ON SCHEMA public TO backup_user;
GRANT SELECT ON ALL TABLES IN SCHEMA public TO backup_user;
GRANT SELECT ON ALL SEQUENCES IN SCHEMA public TO backup_user;

-- For future tables
ALTER DEFAULT PRIVILEGES IN SCHEMA public
GRANT SELECT ON TABLES TO backup_user;
```

### 2. Password Management
- Use strong, unique passwords for each database
- Store passwords in environment variables or secret management systems
- Never commit passwords to version control
- Rotate passwords regularly

### 3. Network Security
- Use SSL/TLS connections (`ssl_mode: "require"` or `"verify-full"`)
- Restrict database access by IP
- Use VPN or private networks for database connections

### 4. S3 Security
- Enable S3 bucket versioning
- Enable server-side encryption
- Use IAM roles instead of static credentials when possible
- Implement bucket lifecycle policies
- Enable S3 access logging

### 5. Dashboard Security
- Use HTTPS in production (`COOKIE_SECURE=true`)
- Set strong JWT secret (minimum 32 characters)
- Implement rate limiting
- Regular security updates
- Monitor audit logs

## 📁 Backup Organization

### S3 Structure
```
s3://your-bucket/
├── backups/
│   ├── production/
│   │   ├── main/
│   │   │   ├── prod_main_20240101_020000.dump
│   │   │   ├── prod_main_20240102_020000.dump
│   │   │   └── prod_main_20240103_020000.dump
│   │   └── analytics/
│   │       ├── analytics_20240101_030000.dump
│   │       └── analytics_20240102_030000.dump
│   ├── staging/
│   │   ├── staging_20240101_040000.dump
│   │   └── staging_20240102_040000.dump
│   └── development/
│       └── dev_20240101_050000.dump
```

### Local Directory
```
./backups/
├── downloads/           # Downloaded backups for restore
│   ├── production_main/
│   └── staging/
└── temp/               # Temporary files (auto-cleaned)
```

## 🔄 Backup Workflow

1. **Create Dump**: Execute `pg_dump` with custom format and compression
2. **Validate**: Run `pg_restore --list` to verify archive integrity
3. **Upload**: Transfer to S3 with retry logic
4. **Verify**: Check S3 object size matches local file
5. **Cleanup**: Remove local file only after successful verification

## ⚡ Performance Optimization

### Parallel Backups
```bash
# Backup 10 databases in parallel
pgbackup -config .env -parallel

# Monitor resource usage
htop  # In another terminal
```

### Compression Levels
- `0`: No compression (fastest, largest files)
- `1-3`: Low compression (fast, moderate size)
- `4-6`: Medium compression (balanced)
- `7-9`: High compression (slowest, smallest files)

### Network Optimization
```bash
# Increase upload chunk size
export AWS_S3_UPLOAD_PART_SIZE=10485760  # 10MB chunks

# Adjust concurrent uploads
export AWS_S3_MAX_CONCURRENT_UPLOADS=10
```

## 📊 Monitoring

### Logs
```bash
# JSON logs (for log aggregation)
LOG_FORMAT=json pgbackup -config .env

# Human-readable logs
LOG_FORMAT=text LOG_LEVEL=debug pgbackup -config .env

# Save to file
pgbackup -config .env 2>&1 | tee backup.log
```

### Metrics to Monitor
- Backup duration per database
- Backup size trends
- Success/failure rates
- S3 storage usage
- Retention policy effectiveness

### Alerting
Configure webhook notifications for failures:
```json
{
  "webhook_url": "https://hooks.slack.com/services/...",
  "webhook_timeout": "10s",
  "notification_enabled": true
}
```

## 🐛 Troubleshooting

### Common Issues

#### 1. Authentication Failed
```bash
# Check credentials
psql -h hostname -U username -d database -c "SELECT 1"

# Verify password
echo $PGPASSWORD
```

#### 2. S3 Upload Fails
```bash
# Test S3 access
aws s3 ls s3://your-bucket/

# Check credentials
aws sts get-caller-identity
```

#### 3. Restore Fails
```bash
# List archive contents
pg_restore -l backup.dump

# Verbose restore
pgrestore -config .env --local backup.dump --verbose --dry-run
```

#### 4. Out of Disk Space
```bash
# Check available space
df -h /path/to/backup/dir

# Clean old local backups
find ./backups -name "*.dump" -mtime +7 -delete
```

### Debug Mode
```bash
# Maximum verbosity
LOG_LEVEL=debug LOG_FORMAT=text pgbackup -config .env 2>&1 | tee debug.log

# Trace pg_dump execution
PGDUMP_VERBOSE=1 pgbackup -config .env
```

## 🚦 Exit Codes

| Code | Description |
|------|-------------|
| 0 | Success |
| 1 | Runtime error (backup/restore failed) |
| 2 | Configuration error |
| 3 | User cancelled (restore confirmation) |

## 📈 Roadmap

### Planned Features
- [ ] Full web UI for multi-database management
- [ ] Real-time backup progress via WebSocket
- [ ] Point-in-time recovery interface
- [ ] Incremental backups support
- [ ] Backup encryption at rest
- [ ] Kubernetes operator
- [ ] Prometheus metrics export
- [ ] Backup verification jobs
- [ ] Cross-region replication
- [ ] Database migration tools

### In Development
- [x] Multi-database support
- [x] Database restore functionality
- [x] User authentication system
- [x] Role-based access control
- [ ] Complete web dashboard UI
- [ ] REST API documentation
- [ ] Terraform modules
- [ ] Docker images

## 🤝 Contributing

We welcome contributions! Please see [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

### Development Setup
```bash
# Clone repository
git clone https://github.com/neelgai/postgres-backup.git
cd postgres-backup

# Install dependencies
go mod download

# Run tests
go test ./...

# Build all binaries
make build

# Run with race detector
go run -race ./cmd/pgbackup
```

## 📄 License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

## 🆘 Support

### Documentation
- [Configuration Guide](docs/configuration.md)
- [Multi-Database Setup](docs/multi-database.md)
- [Restore Guide](docs/restore.md)
- [Security Best Practices](docs/security.md)
- [API Reference](docs/api.md)

### Getting Help
- **Issues**: [GitHub Issues](https://github.com/neelgai/postgres-backup/issues)
- **Discussions**: [GitHub Discussions](https://github.com/neelgai/postgres-backup/discussions)
- **Security**: Report security issues to security@example.com

## 🙏 Acknowledgments

- PostgreSQL team for excellent backup tools
- AWS SDK for Go team
- MongoDB Go driver team
- Go community for amazing libraries

---

**Built with ❤️ for reliable database backups**