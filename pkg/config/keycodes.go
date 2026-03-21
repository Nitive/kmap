package config

import (
	"fmt"
)

const DefaultDevicePath = "/dev/input/by-path/platform-i8042-serio-0-event-kbd"

const (
	KeyEsc        = 1
	Key1          = 2
	Key2          = 3
	Key3          = 4
	Key4          = 5
	Key5          = 6
	Key6          = 7
	Key7          = 8
	Key8          = 9
	Key9          = 10
	Key0          = 11
	KeyMinus      = 12
	KeyEqual      = 13
	KeyBackspace  = 14
	KeyTab        = 15
	KeyQ          = 16
	KeyW          = 17
	KeyE          = 18
	KeyR          = 19
	KeyT          = 20
	KeyY          = 21
	KeyU          = 22
	KeyI          = 23
	KeyO          = 24
	KeyP          = 25
	KeyLeftBrace  = 26
	KeyRightBrace = 27
	KeyEnter      = 28
	KeyLeftCtrl   = 29
	KeyA          = 30
	KeyS          = 31
	KeyD          = 32
	KeyF          = 33
	KeyG          = 34
	KeyH          = 35
	KeyJ          = 36
	KeyK          = 37
	KeyL          = 38
	KeySemicolon  = 39
	KeyApostrophe = 40
	KeyGrv        = 41
	KeyLeftShift  = 42
	KeyBackslash  = 43
	KeyZ          = 44
	KeyX          = 45
	KeyC          = 46
	KeyV          = 47
	KeyB          = 48
	KeyN          = 49
	KeyM          = 50
	KeyComma      = 51
	KeyDot        = 52
	KeySlash      = 53
	KeyRightShift = 54
	KeyLeftAlt    = 56
	KeySpace      = 57
	KeyCapsLock   = 58
	KeyScrollLock = 70
	KeyRightCtrl  = 97
	KeyRightAlt   = 100
	KeyUp         = 103
	KeyLeft       = 105
	KeyRight      = 106
	KeyDown       = 108
	KeyLeftMeta   = 125
	KeyRightMeta  = 126
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
