.PHONY: test run docker ingest

# Coupon files and ingest state for `make ingest`.
COUPON_DIR ?= $(HOME)/oolio-data
STATE ?= $(COUPON_DIR)/state

test:
	test -z "$$(gofmt -l .)" && go vet ./... && go test -race ./...

run:
	API_KEYS="apitest:create_order,readonly:" LOG_FORMAT=text go run ./cmd/server

docker:
	docker compose up --build

ingest:
	go run ./cmd/ingest -state $(STATE) $(COUPON_DIR)/couponbase1.gz $(COUPON_DIR)/couponbase2.gz $(COUPON_DIR)/couponbase3.gz
