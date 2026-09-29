.PHONY: all web build build-go test install clean dist

# Targets for `make dist`, as GOOS-GOARCH. Override with e.g.
#   make dist DIST_TARGETS="linux-amd64"
DIST_TARGETS ?= linux-amd64 linux-arm64 darwin-arm64 darwin-amd64

all: build

web/node_modules: web/package.json
	cd web && npm ci
	touch web/node_modules

web: web/node_modules
	cd web && npm run build

build: web
	go build -o bin/reviewer ./cmd/reviewer

# Build without rebuilding the web UI (uses the previously built UI).
build-go:
	go build -o bin/reviewer ./cmd/reviewer

test:
	go test ./...
	cd web && npm run typecheck

install: web
	go install ./cmd/reviewer

clean:
	rm -rf bin dist

# Cross-compile into dist/<os>-<arch>/reviewer so that each binary keeps its
# name when copied to a remote machine. The UI is built once for all targets.
dist: web
	$(MAKE) $(addprefix dist/,$(addsuffix /reviewer,$(DIST_TARGETS)))

dist/%/reviewer: FORCE
	CGO_ENABLED=0 GOOS=$(word 1,$(subst -, ,$*)) GOARCH=$(word 2,$(subst -, ,$*)) \
		go build -trimpath -o $@ ./cmd/reviewer

.PHONY: FORCE
FORCE:
