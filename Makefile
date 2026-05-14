BINARY  := gdsync
PKG     := ./cmd/gdsync
VERSION ?= 0.1.0-dev
LDFLAGS := -ldflags="-s -w -X main.version=$(VERSION)"

.PHONY: build install test fmt vet tidy clean dist dist-mac dist-win

build:
	go build $(LDFLAGS) -o $(BINARY) $(PKG)

install:
	go install $(LDFLAGS) $(PKG)

test:
	go test ./...

fmt:
	gofmt -w .

vet:
	go vet ./...

tidy:
	go mod tidy

clean:
	rm -rf bin/ $(BINARY) $(BINARY).exe

dist-mac:
	mkdir -p bin
	GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o bin/$(BINARY)-darwin-arm64 $(PKG)
	GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o bin/$(BINARY)-darwin-amd64 $(PKG)

dist-win:
	mkdir -p bin
	GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o bin/$(BINARY)-windows-amd64.exe $(PKG)

dist: dist-mac dist-win
