install:
	sudo systemctl enable ./services/kmap.service
	sudo systemctl start kmap.service

restart: build
	./bin/kmap generate-xcompose --config ./kmap.yaml --output ~/.XCompose
	sudo systemctl restart kmap.service

uninstall:
	sudo systemctl disable --now ./services/kmap.service
	sudo systemctl daemon-reload

build:
	mkdir -p ./bin
	go build -o ./bin/kmap ./cmd

test:
	go test ./...

test-coverage:
	mkdir -p tmp/
	go test -v ./... -covermode=count -coverpkg=./... -coverprofile tmp/coverage.out
	go tool cover -html tmp/coverage.out -o tmp/coverage.html
