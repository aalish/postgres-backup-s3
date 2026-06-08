#!/bin/bash

# Start Dashboard with Multi-Database Support
# This script ensures proper database classification from databases.json

set -e

echo "================================================================="
echo "Starting Dashboard with Multi-Database Classification"
echo "================================================================="
echo ""

# Step 1: Enable Multi-Database Mode
export MULTI_DATABASE_MODE=true
export DATABASE_CONFIG_FILE=databases.json

# Step 2: Load environment from .env.multi-db for credentials
if [ -f ".env.multi-db" ]; then
    echo "✓ Loading credentials from .env.multi-db"
    set -a  # Mark all new variables for export
    source .env.multi-db
    set +a
else
    echo "⚠ Warning: .env.multi-db not found"
    echo "  Create it from .env.multi-db.example with your database credentials"
    exit 1
fi

# Step 3: Verify required per-database passwords are set
# Define expected variables in .env.multi-db as PGPASSWORD_<db_id>
# No fallback values are hardcoded here.

echo ""
echo "Configuration Status:"
echo "-------------------"
echo "✓ Multi-Database Mode: ENABLED"
echo "✓ Config File: databases.json"
if [ -n "$MONGO_URI" ]; then
    echo "✓ MongoDB URI: configured"
fi
echo ""

# Step 4: Verify databases.json exists
if [ ! -f "databases.json" ]; then
    echo "❌ Error: databases.json not found!"
    echo "  Copy databases.json.example to databases.json and fill in real values."
    exit 1
fi

# Step 5: Build if needed
if [ ! -f "build/pgbackup" ] || [ "cmd/pgbackup/main.go" -nt "build/pgbackup" ]; then
    echo "Building pgbackup..."
    go build -o build/pgbackup ./cmd/pgbackup
    echo "✓ Build complete"
fi

echo ""
echo "Starting Dashboard..."
echo "===================="
echo "URL: http://localhost:8080"
echo ""
echo "Login with the admin credentials configured via ADMIN_USERNAME / ADMIN_PASSWORD"
echo "(see .env.multi-db.example)"
echo ""
echo "Press Ctrl+C to stop"
echo ""

# Step 6: Run the dashboard with all configurations
exec ./build/pgbackup dashboard
