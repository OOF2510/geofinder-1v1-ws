#!/bin/bash
set -e

# Get first argument as version
if [ -z "$1" ]; then
  echo "Usage: $0 <version>"
  exit 1
fi
VERSION=${1}

echo "Running gofmt, go mod download and go mod tidy..."
gofmt -w .
go mod download
go mod tidy

echo "Building the project..."
mkdir -p dist
go build -v -x -race -o dist/geofinder-1v1-ws-${VERSION}.x86_64 .
echo "Build completed. Executable is located at dist/geofinder-1v1-ws-${VERSION}.x86_64"
