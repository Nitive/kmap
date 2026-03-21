install:
	-sudo systemctl disable --now kmap-built-in-keyboard.service kmap-external-keyboard.service
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
	go build -o ./bin/kmap ./cmd/kmap
