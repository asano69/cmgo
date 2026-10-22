.PHONY: all test lint vet tools fixtures fmt

GOTEST  := go test -race -timeout 120s
PACKAGES := ./...

all: test lint vet

test:
	$(GOTEST) $(PACKAGES)


lint:
	golangci-lint run $(PACKAGES)

fmt:
	gofmt -w .

vet:
	go vet $(PACKAGES)
	govulncheck $(PACKAGES)

tools:
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	go install golang.org/x/vuln/cmd/govulncheck@latest
