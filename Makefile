BIN_NAME ?= bocker
GOOS ?= linux
GOARCH ?= amd64
CGO_ENABLED ?= 0
LDFLAGS ?= -s -w
VERSION ?= 3.0.7
NFPM ?= go run github.com/goreleaser/nfpm/v2/cmd/nfpm@latest

.DEFAULT_GOAL := help

.PHONY: help build build-cli build-gui test test-completion vet check clean build-cli-deb build-gui-deb release

help:
	@echo Targets:
	@echo "  make build-cli      Build the standalone ./$(BIN_NAME) CLI binary"
	@echo "  make build-gui      Build the GUI bundle with its matching CLI binary"
	@echo "  make build-cli-deb  Build the CLI deb package using nfpm"
	@echo "  make build-gui-deb  Build the GUI deb package using nfpm"
	@echo "  make build          Alias for build-cli"
	@echo "  make check          Run go test and go vet"
	@echo "  make clean          Remove the standalone binary and deb artifacts"

build: build-cli

build-cli: check
	CGO_ENABLED=$(CGO_ENABLED) GOOS=$(GOOS) GOARCH=$(GOARCH) \
		go build -trimpath -ldflags '$(LDFLAGS)' -o '$(BIN_NAME)' ./cmd/bocker
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
	@echo "Current version: $(VERSION)"
	@NEW_VERSION=$$(echo $(VERSION) | awk -F. '{print $$1"."$$2"."$$3+1}'); \
	echo "Bumping version to $$NEW_VERSION..."; \
	sed -i 's/^VERSION ?=.*/VERSION ?= '"$$NEW_VERSION"'/' Makefile; \
	sed -i 's/const Version = ".*/const Version = "'$$NEW_VERSION'"/' internal/bocker/main.go; \
	sed -i 's/version: .*/version: '$$NEW_VERSION'+1/' gui/pubspec.yaml; \
	echo "Building deb packages..."; \
	$(MAKE) clean; \
	VERSION=$$NEW_VERSION $(MAKE) build-cli-deb; \
	VERSION=$$NEW_VERSION PATH="$(PATH):$(HOME)/.local/flutter/bin" $(MAKE) build-gui-deb; \
	echo "Committing and pushing to GitHub..."; \
	git add .; \
	git commit -m "chore: auto release v$$NEW_VERSION"; \
	git push; \
	echo "Creating git tag and GitHub release..."; \
	git tag -a v$$NEW_VERSION -m "Release v$$NEW_VERSION"; \
	git push origin v$$NEW_VERSION; \
	gh release create v$$NEW_VERSION bocker.deb bocker-gui.deb -t "Release v$$NEW_VERSION" -n "Auto-generated release v$$NEW_VERSION"; \
	echo "Release v$$NEW_VERSION published successfully!"
