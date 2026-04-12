package tui

import (
	"testing"

	"github.com/chojs23/lazyagent/internal/model"
)

func makeEvents(n int) []model.Event {
	events := make([]model.Event, n)
	for i := range events {
		events[i] = model.Event{ID: int64(i), Subtype: "PreToolUse"}
	}
	return events
}

func TestSetEvents_CursorAutoFollow(t *testing.T) {
	e := newEvents()
	e.setEvents(makeEvents(100), 500, 400)

	if e.cursor != 99 {
		t.Fatalf("autoFollow cursor: got %d, want 99", e.cursor)
	}
	if e.loadedOffset != 400 {
		t.Fatalf("loadedOffset: got %d, want 400", e.loadedOffset)
	}
	if e.rawCount != 500 {
		t.Fatalf("rawCount: got %d, want 500", e.rawCount)
	}
}

func TestSetEvents_CursorClamped(t *testing.T) {
	e := newEvents()
	e.autoFollow = false
	e.cursor = 200

	e.setEvents(makeEvents(50), 50, 0)

	if e.cursor != 49 {
		t.Fatalf("clamped cursor: got %d, want 49", e.cursor)
	}
}

func TestSetEvents_Empty(t *testing.T) {
	e := newEvents()
	e.setEvents(nil, 0, 0)

	if e.cursor != 0 {
		t.Fatalf("empty cursor: got %d, want 0", e.cursor)
	}
	if len(e.events) != 0 {
		t.Fatalf("empty events: got %d", len(e.events))
	}
}

func TestPrependEvents_ShiftsCursorAndScroll(t *testing.T) {
	e := newEvents()
	e.setEvents(makeEvents(100), 200, 100)
	e.cursor = 10
	e.scroll = 5

	e.prependEvents(makeEvents(50), 50)

	if e.cursor != 60 {
		t.Fatalf("cursor after prepend: got %d, want 60", e.cursor)
	}
	if e.scroll != 55 {
		t.Fatalf("scroll after prepend: got %d, want 55", e.scroll)
	}
	if e.loadedOffset != 50 {
		t.Fatalf("loadedOffset after prepend: got %d, want 50", e.loadedOffset)
	}
	if len(e.events) != 150 {
		t.Fatalf("total events after prepend: got %d, want 150", len(e.events))
	}
}

func TestNeedsOlder(t *testing.T) {
	tests := []struct {
		name         string
		loadedOffset int
		cursor       int
		want         bool
	}{
		{"at top with older available", 100, 0, true},
		{"near top with older available", 100, eventsPageSize/2 - 1, true},
		{"past threshold", 100, eventsPageSize / 2, false},
		{"no older events", 0, 0, false},
		{"no older events mid cursor", 0, 10, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEvents()
			e.autoFollow = false
			e.loadedOffset = tt.loadedOffset
			e.cursor = tt.cursor
			e.events = makeEvents(eventsPageSize)

			if got := e.needsOlder(); got != tt.want {
				t.Fatalf("needsOlder(): got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMoveUp_DecrementsCursor(t *testing.T) {
	e := newEvents()
	e.setEvents(makeEvents(10), 10, 0)
	e.cursor = 5

	e.moveUp()

	if e.cursor != 4 {
		t.Fatalf("cursor after moveUp: got %d, want 4", e.cursor)
	}
	if e.autoFollow {
		t.Fatal("autoFollow should be disabled after moveUp")
	}
}

func TestMoveUp_AtZero(t *testing.T) {
	e := newEvents()
	e.setEvents(makeEvents(10), 10, 0)
	e.autoFollow = false
	e.cursor = 0

	e.moveUp()

	if e.cursor != 0 {
		t.Fatalf("cursor should stay at 0, got %d", e.cursor)
	}
}

func TestMoveDown_IncrementsCursor(t *testing.T) {
	e := newEvents()
	e.setEvents(makeEvents(10), 10, 0)
	e.autoFollow = false
	e.cursor = 5

	e.moveDown()

	if e.cursor != 6 {
		t.Fatalf("cursor after moveDown: got %d, want 6", e.cursor)
	}
}

func TestMoveDown_AtEnd(t *testing.T) {
	e := newEvents()
	e.setEvents(makeEvents(10), 10, 0)
	e.autoFollow = false
	e.cursor = 9

	e.moveDown()

	if e.cursor != 9 {
		t.Fatalf("cursor should stay at 9, got %d", e.cursor)
	}
}

func TestHalfPageUp(t *testing.T) {
	e := newEvents()
	e.setEvents(makeEvents(100), 100, 0)
	e.autoFollow = false
	e.cursor = 50

	e.halfPageUp(20)

	if e.cursor != 40 {
		t.Fatalf("cursor after halfPageUp: got %d, want 40", e.cursor)
	}
}

func TestHalfPageUp_ClampToZero(t *testing.T) {
	e := newEvents()
	e.setEvents(makeEvents(100), 100, 0)
	e.autoFollow = false
	e.cursor = 3

	e.halfPageUp(20)

	if e.cursor != 0 {
		t.Fatalf("cursor should clamp to 0, got %d", e.cursor)
	}
}

func TestHalfPageDown(t *testing.T) {
	e := newEvents()
	e.setEvents(makeEvents(100), 100, 0)
	e.autoFollow = false
	e.cursor = 50

	e.halfPageDown(20)

	if e.cursor != 60 {
		t.Fatalf("cursor after halfPageDown: got %d, want 60", e.cursor)
	}
}

func TestHalfPageDown_ClampToEnd(t *testing.T) {
	e := newEvents()
	e.setEvents(makeEvents(100), 100, 0)
	e.autoFollow = false
	e.cursor = 95

	e.halfPageDown(20)

	if e.cursor != 99 {
		t.Fatalf("cursor should clamp to 99, got %d", e.cursor)
	}
}

func TestGoTop(t *testing.T) {
	e := newEvents()
	e.setEvents(makeEvents(100), 100, 0)
	e.cursor = 50

	e.goTop()

	if e.cursor != 0 {
		t.Fatalf("cursor after goTop: got %d, want 0", e.cursor)
	}
	if e.autoFollow {
		t.Fatal("autoFollow should be disabled after goTop")
	}
}

func TestGoBottom(t *testing.T) {
	e := newEvents()
	e.autoFollow = false
	e.setEvents(makeEvents(100), 100, 0)
	e.cursor = 10

	e.goBottom()

	if e.cursor != 99 {
		t.Fatalf("cursor after goBottom: got %d, want 99", e.cursor)
	}
}

func TestToggleAutoFollow(t *testing.T) {
	e := newEvents()
	e.setEvents(makeEvents(100), 100, 0)
	e.autoFollow = false
	e.cursor = 10

	e.toggleAutoFollow()

	if !e.autoFollow {
		t.Fatal("autoFollow should be true")
	}
	if e.cursor != 99 {
		t.Fatalf("cursor should jump to end: got %d, want 99", e.cursor)
	}

	e.toggleAutoFollow()

	if e.autoFollow {
		t.Fatal("autoFollow should be false")
	}
}

func TestSelectedEvent(t *testing.T) {
	e := newEvents()
	e.setEvents(makeEvents(10), 10, 0)
	e.autoFollow = false
	e.cursor = 3

	ev := e.selectedEvent()
	if ev == nil {
		t.Fatal("selectedEvent should not be nil")
	}
	if ev.ID != 3 {
		t.Fatalf("selectedEvent ID: got %d, want 3", ev.ID)
	}
}

func TestSelectedEvent_Empty(t *testing.T) {
	e := newEvents()

	ev := e.selectedEvent()
	if ev != nil {
		t.Fatal("selectedEvent should be nil for empty model")
	}
}

func TestRenderEventLine_AbsoluteNumbering(t *testing.T) {
	e := newEvents()
	e.setEvents(makeEvents(100), 10000, 9900)

	// The view renders lines with absolute index. Verify by calling
	// renderEventLine directly with an absolute index.
	ev := e.events[0]
	line := e.renderEventLine(ev, 9900, false, nil, 80, 5)

	// The absolute number should be 9901 (9900 + 1 since renderEventLine does index+1)
	if !contains(line, "9901") {
		t.Fatalf("expected absolute number 9901 in line, got: %s", line)
	}
}

func TestRenderEventLine_AbsoluteNumbering_LastEvent(t *testing.T) {
	e := newEvents()
	e.setEvents(makeEvents(100), 10000, 9900)

	ev := e.events[99]
	line := e.renderEventLine(ev, 9999, false, nil, 80, 5)

	if !contains(line, "10000") {
		t.Fatalf("expected absolute number 10000 in line, got: %s", line)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
