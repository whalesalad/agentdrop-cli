.PHONY: build test check dist clean

build:
	CGO_ENABLED=0 go build -trimpath -o agentdrop ./cmd/agentdrop

test:
	go test -race ./...

## check runs everything CI runs: formatting, vet, tests, and a Windows vet.
check:
	@test -z "$$(gofmt -l .)" || (gofmt -l . && echo "gofmt: files need formatting" && exit 1)
	go vet ./...
	GOOS=windows GOARCH=amd64 go vet ./...
	GOOS=darwin GOARCH=arm64 go vet ./...
	go test -race ./...

dist:
	./scripts/build.sh

clean:
	rm -rf dist agentdrop agentdrop.exe
