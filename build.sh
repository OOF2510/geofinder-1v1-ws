#!/bin/bash

set -e

gofmt -w -s .
go mod tidy

mkdir -p dist
go build -o dist/geofinder-1v1-ws.x86_64 .
echo "Build completed. Executable is located at dist/geofinder-1v1-ws.x86_64"