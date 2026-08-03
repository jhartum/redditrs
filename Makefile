BINARY := redditrs
GOFLAGS := -trimpath
LDFLAGS := -s -w

.PHONY: build install test vet fmt lint clean

build:
	go build $(GOFLAGS) -ldflags="$(LDFLAGS)" -o $(BINARY) ./cmd/redditrs

install:
	go install $(GOFLAGS) -ldflags="$(LDFLAGS)" ./cmd/redditrs

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -l -w .

lint:
	golangci-lint run ./...

clean:
	rm -f $(BINARY)
