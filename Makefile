install:
	sudo systemctl enable ./kmonad-built-in-keyboard.service
	sudo systemctl enable ./kmonad-external-keyboard.service
	sudo systemctl start kmonad-built-in-keyboard.service
	sudo systemctl start kmonad-external-keyboard.service

uninstall:
	sudo rm /usr/lib/systemd/system/kmonad-built-in-keyboard.service
	sudo rm /usr/lib/systemd/system/kmonad-external-keyboard.service
	sudo systemctl daemon-reload

build-altremap:
	mkdir -p /home/nitive/Develop/keyboard/bin
	go build -o /home/nitive/Develop/keyboard/bin/altremap ./cmd/altremap

build-layout-capture:
	mkdir -p /home/nitive/Develop/keyboard/bin
	go build -o /home/nitive/Develop/keyboard/bin/layout-capture ./cmd/layout-capture
