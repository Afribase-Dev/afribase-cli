VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
PKG     := github.com/afribase/cli/cmd

LDFLAGS := -s -w \
	-X $(PKG).version=$(VERSION) \
	-X $(PKG).commit=$(COMMIT) \
	-X $(PKG).date=$(DATE)

.PHONY: build install test vet fmt clean snapshot release

build:
	go build -ldflags "$(LDFLAGS)" -o bin/afribase .

# Puts the binary on PATH via the Go bin directory.
install:
	go install -ldflags "$(LDFLAGS)" .

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

clean:
	rm -rf bin dist

# Build every platform locally without publishing, to check the release works.
snapshot:
	goreleaser release --snapshot --clean

# Publishes. Tag first: git tag -a cli/v0.1.0 -m "..." && git push origin cli/v0.1.0
release:
	goreleaser release --clean
