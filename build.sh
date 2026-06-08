#!/bin/bash

# PostgreSQL Backup System - Build Script

set -e

echo "🔨 PostgreSQL Backup System Builder"
echo "===================================="
echo ""

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Check Go installation
if ! command -v go &> /dev/null; then
    echo -e "${RED}❌ Go is not installed. Please install Go 1.25.0 or later${NC}"
    exit 1
fi

echo -e "${GREEN}✅ Go version:${NC} $(go version)"
echo ""

# Build directory
BUILD_DIR="./build"
mkdir -p $BUILD_DIR

# Get version from git or use 'dev'
VERSION=$(git describe --tags --always --dirty 2>/dev/null || echo "dev")

echo "📦 Building applications (version: $VERSION)..."
echo ""

# Build pgbackup
echo -e "${YELLOW}Building pgbackup...${NC}"
go build -ldflags "-X main.version=$VERSION" -o $BUILD_DIR/pgbackup ./cmd/pgbackup
echo -e "${GREEN}✅ pgbackup built successfully${NC}"

# Build pgrestore
echo -e "${YELLOW}Building pgrestore...${NC}"
go build -ldflags "-X main.version=$VERSION" -o $BUILD_DIR/pgrestore ./cmd/pgrestore
echo -e "${GREEN}✅ pgrestore built successfully${NC}"

# Build pgbackupd (Dashboard)
echo -e "${YELLOW}Building pgbackupd (Web Dashboard)...${NC}"
go build -ldflags "-X main.version=$VERSION" -o $BUILD_DIR/pgbackupd ./cmd/pgbackupd
echo -e "${GREEN}✅ pgbackupd built successfully${NC}"

echo ""
echo -e "${GREEN}🎉 Build complete!${NC}"
echo ""
echo "Binaries created in: $BUILD_DIR/"
echo "  • pgbackup  - CLI backup tool"
echo "  • pgrestore - CLI restore tool"
echo "  • pgbackupd - Web dashboard"
echo ""
echo "To run the dashboard:"
echo "  $BUILD_DIR/pgbackupd -config .env.dashboard"
echo ""
echo "To see help for any tool:"
echo "  $BUILD_DIR/pgbackup -h"
echo "  $BUILD_DIR/pgrestore -h"
echo "  $BUILD_DIR/pgbackupd -h"