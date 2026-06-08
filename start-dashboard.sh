#!/bin/bash

# Quick Start Script for PostgreSQL Backup Dashboard

set -e

echo "🚀 PostgreSQL Backup Dashboard - Quick Start"
echo "============================================"
echo ""

# Colors
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

# Check if MongoDB is running
if ! nc -z localhost 27017 2>/dev/null; then
    echo -e "${YELLOW}⚠️  MongoDB is not running on localhost:27017${NC}"
    echo "Starting MongoDB with Docker..."
    docker run -d -p 27017:27017 --name mongodb-pgbackup \
        -e MONGO_INITDB_ROOT_USERNAME=admin \
        -e MONGO_INITDB_ROOT_PASSWORD=password \
        mongo:latest 2>/dev/null || echo "MongoDB container already exists"
    echo "Waiting for MongoDB to start..."
    sleep 5
fi

# Build if binary doesn't exist
if [ ! -f "./build/pgbackupd" ]; then
    echo -e "${YELLOW}Building dashboard...${NC}"
    ./build.sh
fi

# Check for environment file
if [ ! -f ".env.dashboard" ]; then
    echo -e "${YELLOW}⚠️  .env.dashboard not found. Using .env.example${NC}"
    ENV_FILE=".env.example"
else
    ENV_FILE=".env.dashboard"
fi

echo ""
echo -e "${GREEN}✅ Starting Dashboard Server${NC}"
echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo ""
echo "Dashboard URL: http://localhost:8080"
echo "Default Login:"
echo "  Username: admin"
echo "  Password: admin123"
echo ""
echo "Press Ctrl+C to stop the server"
echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo ""

# Start the dashboard
./build/pgbackupd -config $ENV_FILE