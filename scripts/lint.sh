#!/bin/bash -e
# Run from directory above via ./scripts/lint.sh

$(exit $(go fmt ./... | wc -l))
go vet ./...
golint ./...
misspell -error -locale US ./...
