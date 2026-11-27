.PHONY: check gen check-gen check-public check-deps sync-contract release-check

COVERAGE_MIN := 90
COVERPROFILE := $(or $(TMPDIR),/tmp)/yollavoice-coverage.out

# The coverage gate counts statements of the root package only. A package
# without statements (generated types only) passes.
check:
	@test -z "$$(gofmt -l .)" || { gofmt -l .; echo "gofmt: files above need formatting"; exit 1; }
	go vet ./...
	go tool -modfile=tools/go.mod staticcheck ./...
	go test -race -coverprofile="$(COVERPROFILE)" ./...
	@awk -v min=$(COVERAGE_MIN) -v pkg='^github[.]com/yolla-io/yolla-voice-go/[^/]+:' \
		'NR > 1 && $$1 ~ pkg { total += $$2; if ($$3 > 0) covered += $$2 } \
		END { if (total == 0) { print "coverage: no statements in the root package"; exit 0 } \
		pct = 100 * covered / total; printf "coverage: %.1f%% of root package statements (minimum %d%%)\n", pct, min; \
		if (pct < min) exit 1 }' "$(COVERPROFILE)"
	cd tools && go vet ./... && go test -race ./...

gen:
	cd tools && go run ./gen .. ..

check-gen:
	cd tools && go run ./checkgen ..

check-public:
	cd tools && go run ./checkpublic ..

check-deps:
	cd tools && go run ./checkdeps ..

# Usage: make sync-contract SRC=<path to the source openapi.yaml>
sync-contract:
	@test -n "$(SRC)" || { echo "usage: make sync-contract SRC=<path to openapi.yaml>"; exit 1; }
	cd tools && go run ./synccontract "$(abspath $(SRC))" "$(abspath openapi/partner-api.yaml)"

# Usage: make release-check TAG=vX.Y.Z; fails unless the tag equals "v" plus Version in version.go.
release-check:
	@test -n "$(TAG)" || { echo "usage: make release-check TAG=vX.Y.Z"; exit 1; }
	@version=$$(sed -n 's/^const Version = "\(.*\)"$$/\1/p' version.go); \
	test -n "$$version" || { echo "release-check: cannot read Version from version.go"; exit 1; }; \
	test "$(TAG)" = "v$$version" || { echo "release-check: tag $(TAG) does not match Version v$$version"; exit 1; }; \
	echo "release-check: $(TAG) matches Version"
