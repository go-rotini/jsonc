# JSONTestSuite — pin to a specific commit for reproducibility.
# Update the pin intentionally (with a CHANGELOG note) when bumping.
JSONTEST_DIR  := testdata/JSONTestSuite
JSONTEST_REPO := https://github.com/nst/JSONTestSuite.git
JSONTEST_REF  := 1ef36fa01286573e846ac449e8683f8833c5b26a

# tailscale/hujson — pin to a specific commit for reproducibility.
HUJSON_DIR    := testdata/hujson-testdata
HUJSON_REPO   := https://github.com/tailscale/hujson.git
HUJSON_REF    := main

.PHONY: all clean clone-test-suites lint test test-acceptance test-bench \
        test-conformance test-fuzz test-mutation test-race

all: clean clone-test-suites lint test test-acceptance test-bench \
     test-conformance test-fuzz test-mutation test-race

clean:
	@rm -rf $(JSONTEST_DIR) $(HUJSON_DIR) *.out test_mutation.json

clone-test-suites: $(JSONTEST_DIR) $(HUJSON_DIR)

$(JSONTEST_DIR):
	@git clone --quiet $(JSONTEST_REPO) $(JSONTEST_DIR)
	@cd $(JSONTEST_DIR) && git checkout --quiet $(JSONTEST_REF)

$(HUJSON_DIR):
	@git clone --quiet --depth 1 --branch $(HUJSON_REF) $(HUJSON_REPO) $(HUJSON_DIR)

lint:
	@test -z "$$(gofmt -l .)" || (echo "files not formatted:" && gofmt -l . && exit 1)
	go vet ./...
	go mod verify
	go tool golangci-lint run ./...
	go tool go-licenses check ./...
	go tool govulncheck ./...

test: clone-test-suites
	@go test -v -count=1 -coverprofile=test.out .
	@go tool cover -func=test.out | tail -1

test-acceptance:
	@go test -v -count=1 -run TestAcceptance -coverprofile=test_acceptance.out .
	@go tool cover -func=test_acceptance.out | tail -1

test-bench:
	@go test -bench=. -benchmem -count=1 . | tee test_bench.out

test-conformance: clone-test-suites
	@go test -v -count=1 -run 'TestJSONTestSuite|TestJWCCTestSuite|TestJSONCEdgeCases' -coverprofile=test_conformance.out .
	@go tool cover -func=test_conformance.out | tail -1

test-fuzz:
	@go test -fuzz=FuzzUnmarshal -fuzztime=60s -run=^$$ .
	@go test -fuzz=FuzzScanner -fuzztime=60s -run=^$$ .
	@go test -fuzz=FuzzRoundTrip -fuzztime=60s -run=^$$ .
	@go test -fuzz=FuzzValid -fuzztime=60s -run=^$$ .
	@go test -fuzz=FuzzFormat -fuzztime=60s -run=^$$ .
	@go test -fuzz=FuzzMinimize -fuzztime=60s -run=^$$ .

test-mutation: clone-test-suites
	@go tool github.com/go-gremlins/gremlins/cmd/gremlins unleash --config .gremlins.yaml

test-race:
	@go test -race -count=1 -coverprofile=test_race.out .
	@go tool cover -func=test_race.out | tail -1
