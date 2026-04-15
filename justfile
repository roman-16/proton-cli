# Build the binary
build:
    go build -o proton-cli .

# Run integration tests (requires PROTON_USER and PROTON_PASSWORD)
test:
    go test ./tests/ -v -count=1 -timeout 10m

# Run a single test
test-one name:
    go test ./tests/ -v -count=1 -run {{name}} -timeout 5m

# Run go vet
vet:
    go vet ./...

# Build and run
run *args:
    go run . {{args}}

# Clean build artifacts
clean:
    rm -f proton-cli
