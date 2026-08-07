BIN_NAME ?= bocker
GOOS ?= linux
GOARCH ?= amd64
CGO_ENABLED ?= 0
LDFLAGS ?= -s -w

.DEFAULT_GOAL := help

.PHONY: help build build-cli build-gui test vet check clean

help:
	@echo Targets:
	@echo "  make build-cli  Build the standalone ./$(BIN_NAME) CLI binary"
	@echo "  make build-gui  Build the GUI bundle with its matching CLI binary"
	@echo "  make build      Alias for build-cli (backward compatible)"
	@echo "  make check  Run go test and go vet"
	@echo "  make clean  Remove the standalone binary"

build: build-cli

build-cli: check
	CGO_ENABLED=$(CGO_ENABLED) GOOS=$(GOOS) GOARCH=$(GOARCH) \
		go build -trimpath -ldflags '$(LDFLAGS)' -o '$(BIN_NAME)' ./cmd/bocker
	@echo "Built ./$(BIN_NAME) for $(GOOS)/$(GOARCH)"

build-gui:
	./gui/build_release.sh

test:
	go test ./...

vet:
	go vet ./...

check: test vet

clean:
	rm -f '$(BIN_NAME)'
