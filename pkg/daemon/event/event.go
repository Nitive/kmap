package event

type Kind int

const (
	KindKey Kind = iota
	KindLayoutSwitch
)

type LayoutSwitchRequest struct {
	SourceCode uint16
}

// KeyEvent represents a key press/release event or an internal control event.
type KeyEvent struct {
	Kind         Kind
	Code         uint16
	Value        int32
	LayoutSwitch LayoutSwitchRequest
}
