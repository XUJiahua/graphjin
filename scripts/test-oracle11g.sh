#!/bin/bash
set -e
cd "$(dirname "$0")/../tests"
echo "Running Oracle 11g tests..."
go test -v -timeout 30m -race -count=1 -db=oracle11g -run '^TestOracle11g' "$@" .
