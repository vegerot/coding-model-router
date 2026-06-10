.PHONY: build test vet live-test fmt clean

build:
	go build -o router ./cmd/router

test:
	go test ./...

vet:
	go vet ./...

# Live shape-contract tests: fetch the real AA / models.dev / OpenRouter endpoints
# and assert our shape assumptions still hold. Never run by default `go test`.
live-test:
	go test -tags live ./...

fmt:
	gofmt -w .

clean:
	rm -f router
