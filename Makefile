install:
	sudo systemctl enable ./services/kmonad-built-in-keyboard.service
	sudo systemctl enable ./services/kmonad-external-keyboard.service
	sudo systemctl start kmonad-built-in-keyboard.service
	sudo systemctl start kmonad-external-keyboard.service

uninstall:
	sudo rm /usr/lib/systemd/system/kmonad-built-in-keyboard.service
	sudo rm /usr/lib/systemd/system/kmonad-external-keyboard.service
	sudo systemctl daemon-reload

install-altremap:
	sudo systemctl enable ./services/altremap-built-in-keyboard.service
	sudo systemctl enable ./services/altremap-external-keyboard.service
	sudo systemctl start altremap-built-in-keyboard.service
	sudo systemctl start altremap-external-keyboard.service

restart:
	/home/nitive/Develop/keyboard/bin/altremap --config /home/nitive/Develop/keyboard/altremap.yaml --generate-xcompose /home/nitive/.XCompose
	sudo systemctl restart altremap-built-in-keyboard.service altremap-external-keyboard.service

uninstall-altremap:
	sudo rm /usr/lib/systemd/system/altremap-built-in-keyboard.service
	sudo rm /usr/lib/systemd/system/altremap-external-keyboard.service
	sudo systemctl daemon-reload

build-altremap:
	mkdir -p /home/nitive/Develop/keyboard/bin
	go build -o /home/nitive/Develop/keyboard/bin/altremap ./cmd/altremap

build-layout-capture:
	mkdir -p /home/nitive/Develop/keyboard/bin
	go build -o /home/nitive/Develop/keyboard/bin/layout-capture ./cmd/layout-capture
