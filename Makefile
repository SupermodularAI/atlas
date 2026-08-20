.PHONY: test build lint example all release clean-release
all: lint test build

test:
	go test ./...

build:
	go build -o atlas ./cmd/atlas

lint:
	go vet ./...

example: build
	./examples/run-example.sh

# --- release -----------------------------------------------------------------
#
# Cross-compiles pinned, reproducible binaries and a checksum file, for
# attaching to a GitHub Release.
#
# Why reproducible flags matter here rather than being fastidious: a consumer
# pins the SHA256, not the tag. A tag is a mutable pointer — `git tag -f` moves
# it and a fetch silently gets different code — whereas a checksum cannot be
# moved. That only holds if the same source yields the same bytes, so:
#
#   -trimpath        strips local filesystem paths from the binary
#   -buildvcs=false  omits VCS state, which differs per checkout
#   -ldflags "-s -w" drops symbol/debug tables (smaller, and stable)
#
# Without those, the checksum varies by who built it and pinning proves nothing.
#
# VERSION is stamped in so a published atlas can say which tool made it.
# Override on the command line: make release VERSION=v0.1.0

VERSION ?= dev
RELEASE_DIR := dist/release
GO_LDFLAGS := -s -w -X main.version=$(VERSION)
PLATFORMS := linux/amd64 darwin/arm64 darwin/amd64

release: clean-release
	@mkdir -p $(RELEASE_DIR)
	@for p in $(PLATFORMS); do \
		os=$${p%/*}; arch=$${p#*/}; \
		out="$(RELEASE_DIR)/atlas-$(VERSION)-$$os-$$arch"; \
		echo "building $$out"; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch \
			go build -trimpath -buildvcs=false -ldflags "$(GO_LDFLAGS)" \
			-o "$$out" ./cmd/atlas || exit 1; \
	done
	@cd $(RELEASE_DIR) && shasum -a 256 atlas-* > SHA256SUMS
	@echo
	@echo "$(RELEASE_DIR)/SHA256SUMS:"
	@cat $(RELEASE_DIR)/SHA256SUMS
	@echo
	@echo "Attach every file in $(RELEASE_DIR)/ to the release, SHA256SUMS included."
	@echo "CI pins the checksum, so SHA256SUMS is the artifact that makes the"
	@echo "binary verifiable rather than merely downloadable."

clean-release:
	@rm -rf $(RELEASE_DIR)
