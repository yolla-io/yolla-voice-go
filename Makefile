.PHONY: check check-public check-deps sync-contract

check:
	@test -z "$$(gofmt -l .)" || { gofmt -l .; echo "gofmt: files above need formatting"; exit 1; }
	go vet ./...
	go test -race ./...

check-public:
	cd tools && go run ./checkpublic ..

check-deps:
	cd tools && go run ./checkdeps ..

# Usage: make sync-contract SRC=<path to the source openapi.yaml>
sync-contract:
	@test -n "$(SRC)" || { echo "usage: make sync-contract SRC=<path to openapi.yaml>"; exit 1; }
	cd tools && go run ./synccontract "$(abspath $(SRC))" "$(abspath openapi/partner-api.yaml)"
