MISE_ENV_MK := $(abspath ./tmp/mise.env.mk)
$(shell mkdir -p $(dir $(MISE_ENV_MK)) >/dev/null 2>&1 && mise env --json | jq -r 'to_entries[] | "export \(.key) := \(.value)"' > $(MISE_ENV_MK))

include $(MISE_ENV_MK)

export GOCACHE := $(abspath ./tmp/go-cache)
export GOLANGCI_LINT_CACHE := $(abspath ./tmp/golangci-lint-cache)
USER_BIN_DIR := $(HOME)/.local/bin
USER_SYSTEMD_DIR := $(HOME)/.config/systemd/user

.PHONY: install restart uninstall build test test-coverage lint fmt cache-dirs

cache-dirs:
	mkdir -p $(GOCACHE) $(GOLANGCI_LINT_CACHE)

install: build
	mkdir -p $(USER_BIN_DIR) $(USER_SYSTEMD_DIR)
	install -m 755 ./bin/kmap $(USER_BIN_DIR)/kmap
	install -m 644 ./services/kmap.service $(USER_SYSTEMD_DIR)/kmap.service
	systemctl --user daemon-reload
	systemctl --user enable --now kmap.service

restart: build
	install -m 755 ./bin/kmap $(USER_BIN_DIR)/kmap
	install -m 644 ./services/kmap.service $(USER_SYSTEMD_DIR)/kmap.service
	systemctl --user daemon-reload
	systemctl --user restart kmap.service

uninstall:
	systemctl --user disable --now kmap.service || true
	rm -f $(USER_SYSTEMD_DIR)/kmap.service $(USER_BIN_DIR)/kmap
	systemctl --user daemon-reload

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
