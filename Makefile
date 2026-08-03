BIN_NAME ?= bocker
GOOS ?= linux
GOARCH ?= amd64
CGO_ENABLED ?= 0
LDFLAGS ?= -s -w

.DEFAULT_GOAL := help

.PHONY: help build test vet check clean

help:
	@echo Targets:
	@echo "  make build  Build the single standalone ./$(BIN_NAME) binary"
	@echo "  make check  Run go test and go vet"
	@echo "  make clean  Remove the standalone binary"

build: check
	CGO_ENABLED=$(CGO_ENABLED) GOOS=$(GOOS) GOARCH=$(GOARCH) \
		go build -trimpath -ldflags '$(LDFLAGS)' -o '$(BIN_NAME)' .
	@echo "Built ./$(BIN_NAME) for $(GOOS)/$(GOARCH)"

test:
	go test ./...

vet:
	go vet ./...

check: test vet

clean:
	rm -f '$(BIN_NAME)'
