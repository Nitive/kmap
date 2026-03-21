install:
	sudo systemctl enable ./services/kmap-built-in-keyboard.service ./services/kmap-external-keyboard.service
	sudo systemctl start kmap-built-in-keyboard.service kmap-external-keyboard.service

restart: build
	./bin/kmap generate-xcompose --config ./kmap.yaml --output ~/.XCompose
	sudo systemctl restart kmap-built-in-keyboard.service kmap-external-keyboard.service

uninstall:
	sudo systemctl disable --now ./services/kmap-built-in-keyboard.service ./services/kmap-external-keyboard.service
	sudo systemctl daemon-reload

build:
	mkdir -p ./bin
	go build -o ./bin/kmap ./cmd/kmap
