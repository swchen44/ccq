# ccq — build, test, lint, release
.PHONY: build test test-integration lint fmt vet release clean install

build:
	go build -o ccq ./cmd/ccq

test:                 ## unit tests (no clangd needed)
	go test ./...

test-integration:     ## end-to-end tests (requires clangd on PATH)
	go test -tags integration ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

lint:                 ## go vet + golangci-lint (if installed)
	go vet ./...
	@command -v golangci-lint >/dev/null && golangci-lint run || echo "golangci-lint not installed; ran go vet only"

release:              ## cross-compile all platforms into dist/
	./build-release.sh

install:
	./install.sh

clean:
	rm -rf ccq ccq.exe dist
