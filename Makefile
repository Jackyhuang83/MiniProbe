VERSION ?= 0.4.6-alpha
LDFLAGS := -s -w
.PHONY: build release test clean

clean:
	rm -rf dist

build:
	mkdir -p dist
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o dist/miniprobe-server ./cmd/miniprobe-server
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o dist/miniprobe-agent ./cmd/miniprobe-agent

release: clean
	mkdir -p dist
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o dist/miniprobe-server-linux-amd64 ./cmd/miniprobe-server
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o dist/miniprobe-server-linux-arm64 ./cmd/miniprobe-server
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o dist/miniprobe-agent-linux-amd64 ./cmd/miniprobe-agent
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o dist/miniprobe-agent-linux-arm64 ./cmd/miniprobe-agent
	CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 go build -ldflags "$(LDFLAGS)" -o dist/miniprobe-agent-linux-armv7 ./cmd/miniprobe-agent
	(cd dist && sha256sum miniprobe-* > SHA256SUMS)

test:
	go test ./...
