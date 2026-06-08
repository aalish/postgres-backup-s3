#!/bin/bash

# Multi-Database Backup Script
# Trigger a backup for a specific database ID (as defined in databases.json),
# or "all" to back up every enabled database.

set -e

GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
RED='\033[0;31m'
NC='\033[0m'

usage() {
    echo "Usage: $0 <database_id|all>"
    echo ""
    echo "  database_id   The 'id' of a database entry in databases.json."
    echo "  all           Back up every enabled database."
    echo ""
    echo "Examples:"
    echo "  $0 production_main"
    echo "  $0 all"
    exit 1
}

if [ $# -eq 0 ]; then
    usage
fi

DATABASE=$1

echo -e "${BLUE}═══════════════════════════════════════════════════════════════${NC}"
echo -e "${BLUE}    Multi-Database Backup Utility${NC}"
echo -e "${BLUE}═══════════════════════════════════════════════════════════════${NC}"
echo ""

# Build tools if needed
if [ ! -f "./build/pgbackup" ]; then
    echo -e "${YELLOW}Building backup tool...${NC}"
    go build -o build/pgbackup ./cmd/pgbackup
fi

if [ "$DATABASE" = "all" ]; then
    echo -e "${GREEN}Starting backup for ALL databases...${NC}"
    ./build/pgbackup -config .env.multi-db -parallel
else
    echo -e "${GREEN}Starting backup for database: ${DATABASE}${NC}"
    ./build/pgbackup -config .env.multi-db -database "$DATABASE"
fi

echo ""
echo -e "${GREEN}✅ Backup operation completed!${NC}"
echo ""
echo "To list backups:"
echo "  ./build/pgrestore -config .env.multi-db --list"
