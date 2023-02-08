#!/usr/bin/env sh

go test -coverprofile=coverage.out -v ./... || exit 1
go tool cover -func coverage.out
