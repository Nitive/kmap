install:
	sudo systemctl enable ./kmonad-built-in-keyboard.service
	sudo systemctl enable ./kmonad-external-keyboard.service
	sudo systemctl start kmonad-built-in-keyboard.service
	sudo systemctl start kmonad-external-keyboard.service

uninstall:
	sudo rm /usr/lib/systemd/system/kmonad-built-in-keyboard.service
	sudo rm /usr/lib/systemd/system/kmonad-external-keyboard.service
	sudo systemctl daemon-reload