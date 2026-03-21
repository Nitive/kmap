package config

import (
	"fmt"

	"golang.org/x/sys/unix"
)

const DefaultDevicePath = "/dev/input/by-path/platform-i8042-serio-0-event-kbd"

const (
	KeyEsc        = unix.KEY_ESC
	Key1          = unix.KEY_1
	Key2          = unix.KEY_2
	Key3          = unix.KEY_3
	Key4          = unix.KEY_4
	Key5          = unix.KEY_5
	Key6          = unix.KEY_6
	Key7          = unix.KEY_7
	Key8          = unix.KEY_8
	Key9          = unix.KEY_9
	Key0          = unix.KEY_0
	KeyMinus      = unix.KEY_MINUS
	KeyEqual      = unix.KEY_EQUAL
	KeyBackspace  = unix.KEY_BACKSPACE
	KeyTab        = unix.KEY_TAB
	KeyQ          = unix.KEY_Q
	KeyW          = unix.KEY_W
	KeyE          = unix.KEY_E
	KeyR          = unix.KEY_R
	KeyT          = unix.KEY_T
	KeyY          = unix.KEY_Y
	KeyU          = unix.KEY_U
	KeyI          = unix.KEY_I
	KeyO          = unix.KEY_O
	KeyP          = unix.KEY_P
	KeyLeftBrace  = unix.KEY_LEFTBRACE
	KeyRightBrace = unix.KEY_RIGHTBRACE
	KeyEnter      = unix.KEY_ENTER
	KeyLeftCtrl   = unix.KEY_LEFTCTRL
	KeyA          = unix.KEY_A
	KeyS          = unix.KEY_S
	KeyD          = unix.KEY_D
	KeyF          = unix.KEY_F
	KeyG          = unix.KEY_G
	KeyH          = unix.KEY_H
	KeyJ          = unix.KEY_J
	KeyK          = unix.KEY_K
	KeyL          = unix.KEY_L
	KeySemicolon  = unix.KEY_SEMICOLON
	KeyApostrophe = unix.KEY_APOSTROPHE
	KeyGrv        = unix.KEY_GRAVE
	KeyLeftShift  = unix.KEY_LEFTSHIFT
	KeyBackslash  = unix.KEY_BACKSLASH
	KeyZ          = unix.KEY_Z
	KeyX          = unix.KEY_X
	KeyC          = unix.KEY_C
	KeyV          = unix.KEY_V
	KeyB          = unix.KEY_B
	KeyN          = unix.KEY_N
	KeyM          = unix.KEY_M
	KeyComma      = unix.KEY_COMMA
	KeyDot        = unix.KEY_DOT
	KeySlash      = unix.KEY_SLASH
	KeyRightShift = unix.KEY_RIGHTSHIFT
	KeyLeftAlt    = unix.KEY_LEFTALT
	KeySpace      = unix.KEY_SPACE
	KeyCapsLock   = unix.KEY_CAPSLOCK
	KeyScrollLock = unix.KEY_SCROLLLOCK
	KeyRightCtrl  = unix.KEY_RIGHTCTRL
	KeyRightAlt   = unix.KEY_RIGHTALT
	KeyUp         = unix.KEY_UP
	KeyLeft       = unix.KEY_LEFT
	KeyRight      = unix.KEY_RIGHT
	KeyDown       = unix.KEY_DOWN
	KeyLeftMeta   = unix.KEY_LEFTMETA
	KeyRightMeta  = unix.KEY_RIGHTMETA
)

var keyNameByCode = map[uint16]string{
	KeyEsc:        "KEY_ESC",
	Key1:          "KEY_1",
	Key2:          "KEY_2",
	Key3:          "KEY_3",
	Key4:          "KEY_4",
	Key5:          "KEY_5",
	Key6:          "KEY_6",
	Key7:          "KEY_7",
	Key8:          "KEY_8",
	Key9:          "KEY_9",
	Key0:          "KEY_0",
	KeyMinus:      "KEY_MINUS",
	KeyEqual:      "KEY_EQUAL",
	KeyBackspace:  "KEY_BACKSPACE",
	KeyTab:        "KEY_TAB",
	KeyQ:          "KEY_Q",
	KeyW:          "KEY_W",
	KeyE:          "KEY_E",
	KeyR:          "KEY_R",
	KeyT:          "KEY_T",
	KeyY:          "KEY_Y",
	KeyU:          "KEY_U",
	KeyI:          "KEY_I",
	KeyO:          "KEY_O",
	KeyP:          "KEY_P",
	KeyLeftBrace:  "KEY_LEFTBRACE",
	KeyRightBrace: "KEY_RIGHTBRACE",
	KeyEnter:      "KEY_ENTER",
	KeyLeftCtrl:   "KEY_LEFTCTRL",
	KeyA:          "KEY_A",
	KeyS:          "KEY_S",
	KeyD:          "KEY_D",
	KeyF:          "KEY_F",
	KeyG:          "KEY_G",
	KeyH:          "KEY_H",
	KeyJ:          "KEY_J",
	KeyK:          "KEY_K",
	KeyL:          "KEY_L",
	KeySemicolon:  "KEY_SEMICOLON",
	KeyApostrophe: "KEY_APOSTROPHE",
	KeyGrv:        "KEY_GRAVE",
	KeyLeftShift:  "KEY_LEFTSHIFT",
	KeyBackslash:  "KEY_BACKSLASH",
	KeyZ:          "KEY_Z",
	KeyX:          "KEY_X",
	KeyC:          "KEY_C",
	KeyV:          "KEY_V",
	KeyB:          "KEY_B",
	KeyN:          "KEY_N",
	KeyM:          "KEY_M",
	KeyComma:      "KEY_COMMA",
	KeyDot:        "KEY_DOT",
	KeySlash:      "KEY_SLASH",
	KeyRightShift: "KEY_RIGHTSHIFT",
	KeyLeftAlt:    "KEY_LEFTALT",
	KeySpace:      "KEY_SPACE",
	KeyCapsLock:   "KEY_CAPSLOCK",
	KeyRightCtrl:  "KEY_RIGHTCTRL",
	KeyRightAlt:   "KEY_RIGHTALT",
	KeyLeftMeta:   "KEY_LEFTMETA",
	KeyRightMeta:  "KEY_RIGHTMETA",
}

func KeyName(code uint16) string {
	if n, ok := keyNameByCode[code]; ok {
		return n
	}
	return fmt.Sprintf("KEY_%d", code)
}
