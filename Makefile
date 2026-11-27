.PHONY: check check-public check-deps

check:
	@test -z "$$(gofmt -l .)" || { gofmt -l .; echo "gofmt: files above need formatting"; exit 1; }
	go vet ./...
	go test -race ./...

check-public:
	cd tools && go run ./checkpublic ..

check-deps:
	cd tools && go run ./checkdeps ..
