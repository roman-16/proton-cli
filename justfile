# Build the binary
build:
    go build -o proton-cli .

# Clean build artifacts
clean:
    rm -f proton-cli

# Lint and format
lint:
    gofmt -w .
    golangci-lint run ./...

# Build and run
run *args:
    go run . {{args}}

# Run integration tests (requires PROTON_USER and PROTON_PASSWORD)
test:
    go test ./tests/ -v -count=1 -timeout 10m

# Run a single test
test-one name:
    go test ./tests/ -v -count=1 -run {{name}} -timeout 5m
