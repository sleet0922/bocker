BIN_NAME ?= bocker
GOOS ?= linux
GOARCH ?= amd64
CGO_ENABLED ?= 0
LDFLAGS ?= -s -w
VERSION ?= 3.1.4
NFPM_VERSION ?= v2.45.0
NFPM ?= go run github.com/goreleaser/nfpm/v2/cmd/nfpm@$(NFPM_VERSION)

.DEFAULT_GOAL := help

.PHONY: help build build-cli build-gui test test-completion vet check clean build-cli-deb build-gui-deb release publish-release

help:
	@echo Targets:
	@echo "  make build-cli      Build the standalone ./$(BIN_NAME) CLI binary"
	@echo "  make build-gui      Build the GUI bundle with its matching CLI binary"
	@echo "  make build-cli-deb  Build the CLI deb package using nfpm"
	@echo "  make build-gui-deb  Build the GUI deb package using nfpm"
	@echo "  make build          Alias for build-cli"
	@echo "  make check          Run Go tests, completion tests, and go vet"
	@echo "  make release        Build release packages without changing Git state"
	@echo "  make publish-release PUBLISH=1  Publish already-built packages intentionally"
	@echo "  make clean          Remove the standalone binary and deb artifacts"

build: build-cli

build-cli: check
	CGO_ENABLED=$(CGO_ENABLED) GOOS=$(GOOS) GOARCH=$(GOARCH) \
		go build -a -trimpath -ldflags '$(LDFLAGS)' -o '$(BIN_NAME)' ./cmd/bocker
	chmod 0755 '$(BIN_NAME)'
	@echo "Built ./$(BIN_NAME) for $(GOOS)/$(GOARCH)"

build-gui:
	./gui/build_release.sh

test:
	go test ./...

test-completion:
	bash -n completions/bocker
	bash completions/bocker_test.bash

vet:
	go vet ./...

check: test test-completion vet

clean:
	rm -f '$(BIN_NAME)'
	rm -f *.deb

build-cli-deb: build-cli
	@echo "Building bocker CLI deb package..."
	VERSION=$(VERSION) GOARCH=$(GOARCH) $(NFPM) package --config build/nfpm-cli.yaml --packager deb --target bocker.deb

build-gui-deb: build-gui
	@echo "Building bocker GUI deb package..."
	VERSION=$(VERSION) GOARCH=$(GOARCH) $(NFPM) package --config build/nfpm-gui.yaml --packager deb --target bocker-gui.deb

release:
	$(MAKE) build-cli-deb
	$(MAKE) build-gui-deb

publish-release:
	@test "$(PUBLISH)" = "1" || (echo "Set PUBLISH=1 to publish a release" >&2; exit 2)
	@test -f bocker.deb && test -f bocker-gui.deb || (echo "Run make release first" >&2; exit 2)
	@git diff --quiet && git diff --cached --quiet || (echo "Commit or stash changes before publishing" >&2; exit 2)
	@git tag -a "v$(VERSION)" -m "Release v$(VERSION)"
	@git push origin "v$(VERSION)"
	@gh release create "v$(VERSION)" bocker.deb bocker-gui.deb -t "Release v$(VERSION)"
