.PHONY: build check test race cross clean

build:
	go build -buildvcs=false -trimpath -o dist/jumpotp ./cmd/jumpotp

test:
	go test ./...

race:
	go test -race ./...

cross:
	./scripts/build.sh

check:
	test -z "$$(gofmt -l cmd internal)"
	go vet ./...
	go test ./...

clean:
	rm -rf dist coverage
