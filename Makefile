VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "v0.1.0-dev")
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || echo "unknown")

LDFLAGS := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)

.PHONY: all build test lint clean pages check-drift check-overflow

all: build

build:
	go build -ldflags="$(LDFLAGS)" -o bin/badgeboard ./cmd/badgeboard

test:
	go test -race -cover ./...

lint:
	golangci-lint run ./...

pages:
	go run scripts/build_pages.go

check-drift: pages
	git diff --exit-code docs/app.js docs/index.html docs/style.css docs/render.js docs/report.js docs/csv.js docs/demo-data.js

check-overflow:
	node scripts/check_overflow.js

clean:
	rm -rf bin/
