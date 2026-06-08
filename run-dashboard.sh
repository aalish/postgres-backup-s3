#!/bin/bash

# Run the dashboard with multi-database support.
# Reads runtime config from .env.multi-db and databases.json.

set -e

echo "================================================================="
echo "🚀 Starting PostgreSQL Backup Dashboard"
echo "================================================================="
echo ""

# Check if pgbackupd is built
if [ ! -f "./build/pgbackupd" ]; then
    echo "Building pgbackupd..."
    ./build.sh
fi

echo "✅ Build Status:"
echo "  - pgbackup: $([ -f ./build/pgbackup ] && echo '✓' || echo '✗')"
echo "  - pgrestore: $([ -f ./build/pgrestore ] && echo '✓' || echo '✗')"
echo "  - pgbackupd: $([ -f ./build/pgbackupd ] && echo '✓' || echo '✗')"
echo ""

echo "📋 Configuration:"
echo "  - Config file: .env.multi-db"
echo "  - Multi-DB mode: ENABLED"
echo "  - Database config: databases.json"
echo ""

echo "Configured databases are read from databases.json at startup."
echo "Login credentials come from ADMIN_USERNAME / ADMIN_PASSWORD in .env.multi-db."
echo ""
echo "🌐 Dashboard URL: http://localhost:8080"
echo ""

echo "Press Ctrl+C to stop the dashboard"
echo "================================================================="
echo ""

# Start the dashboard with multi-database configuration
exec ./build/pgbackupd -config .env.multi-db
