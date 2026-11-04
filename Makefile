.PHONY: all web build build-go test install clean dist

# Targets for `make dist`, as GOOS-GOARCH. Override with e.g.
#   make dist DIST_TARGETS="linux-amd64"
DIST_TARGETS ?= linux-amd64 linux-arm64 darwin-arm64 darwin-amd64

VERSION ?= dev
GO_LDFLAGS = -X main.version=$(VERSION)

all: build

web/node_modules: web/package.json
	cd web && npm ci
	touch web/node_modules

web: web/node_modules
	cd web && npm run build

build: web
	go build -ldflags "$(GO_LDFLAGS)" -o bin/reviewer ./cmd/reviewer

# Build without rebuilding the web UI (uses the previously built UI).
build-go:
	go build -ldflags "$(GO_LDFLAGS)" -o bin/reviewer ./cmd/reviewer

test:
	go test ./...
	cd web && npm run typecheck

.PHONY: e2e
e2e:
	cd web && npm run e2e

install: web
	go install -ldflags "$(GO_LDFLAGS)" ./cmd/reviewer

clean:
	rm -rf bin dist

# Cross-compile into dist/<os>-<arch>/reviewer so that each binary keeps its
# name when copied to a remote machine. The UI is built once for all targets.
dist: web
	$(MAKE) $(addprefix dist/,$(addsuffix /reviewer,$(DIST_TARGETS)))

dist/%/reviewer: FORCE
	CGO_ENABLED=0 GOOS=$(word 1,$(subst -, ,$*)) GOARCH=$(word 2,$(subst -, ,$*)) \
		go build -trimpath -ldflags "$(GO_LDFLAGS)" -o $@ ./cmd/reviewer

.PHONY: FORCE
FORCE:

# Build versioned release archives and their SHA-256 checksums.
.PHONY: package
package: dist
	@test "$(VERSION)" != dev || (echo "Set VERSION to a release version"; exit 1)
	mkdir -p dist/release
	@for target in $(DIST_TARGETS); do \
		tar -czf "dist/release/reviewer-$(VERSION)-$$target.tar.gz" -C "dist/$$target" reviewer || exit 1; \
	done
	cd dist/release && shasum -a 256 reviewer-$(VERSION)-*.tar.gz > reviewer-$(VERSION)-checksums.txt
