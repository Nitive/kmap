MISE_ENV_MK := $(abspath ./tmp/mise.env.mk)
$(shell mkdir -p $(dir $(MISE_ENV_MK)) >/dev/null 2>&1 && mise env --json | jq -r 'to_entries[] | "export \(.key) := \(.value)"' > $(MISE_ENV_MK))

include $(MISE_ENV_MK)

export GOCACHE := $(abspath ./tmp/go-cache)
export GOLANGCI_LINT_CACHE := $(abspath ./tmp/golangci-lint-cache)

.PHONY: install restart uninstall build test test-coverage lint fmt cache-dirs

cache-dirs:
	mkdir -p $(GOCACHE) $(GOLANGCI_LINT_CACHE)

install:
	sudo systemctl enable ./services/kmap.service
	sudo systemctl start kmap.service

restart: build
	./bin/kmap generate-xcompose --config ./kmap.yaml --output ~/.XCompose
	sudo systemctl restart kmap.service

uninstall:
	sudo systemctl disable --now ./services/kmap.service
	sudo systemctl daemon-reload

build: cache-dirs
	mkdir -p ./bin
	go build -o ./bin/kmap ./cmd

test: cache-dirs
	go test ./...

test-coverage: cache-dirs
	mkdir -p tmp/
	go test -v ./... -covermode=count -coverpkg=./... -coverprofile tmp/coverage.out
	go tool cover -html tmp/coverage.out -o tmp/coverage.html

lint: cache-dirs
	golangci-lint run ./...

fmt: cache-dirs
	golangci-lint fmt -E gofmt
