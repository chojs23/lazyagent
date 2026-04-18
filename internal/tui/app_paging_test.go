package tui

import (
	"reflect"
	"testing"
	"time"

	"github.com/chojs23/lazyagent/internal/model"
)

func TestCurrentEventFilterBuildsFromSelectedState(t *testing.T) {
	m := newModel(nil, time.Second)
	m.agents.selectedAgent = "agent-123"
	m.filter.typeIndex = 2
	m.filter.searchQuery = "needle"

	filter := m.currentEventFilter()

	want := model.EventFilter{
		AgentIDs: []string{"agent-123"},
		Type:     m.filter.typeValue(),
		Search:   "needle",
	}
	if !reflect.DeepEqual(filter, want) {
		t.Fatalf("currentEventFilter() = %#v, want %#v", filter, want)
	}
}

func TestCurrentEventFilterOmitsEmptyAgentSelection(t *testing.T) {
	m := newModel(nil, time.Second)
	m.filter.searchQuery = "text"

	filter := m.currentEventFilter()

	if len(filter.AgentIDs) != 0 {
		t.Fatalf("AgentIDs = %#v, want empty", filter.AgentIDs)
	}
	if filter.Search != "text" {
		t.Fatalf("Search = %q, want text", filter.Search)
	}
}

func TestEventFilterActive(t *testing.T) {
	tests := []struct {
		name   string
		filter model.EventFilter
		want   bool
	}{
		{name: "empty", filter: model.EventFilter{}, want: false},
		{name: "agent", filter: model.EventFilter{AgentIDs: []string{"agent-1"}}, want: true},
		{name: "type", filter: model.EventFilter{Type: "tool"}, want: true},
		{name: "search", filter: model.EventFilter{Search: "needle"}, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := eventFilterActive(tt.filter); got != tt.want {
				t.Fatalf("eventFilterActive() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCurrentRefreshEventWindow(t *testing.T) {
	tests := []struct {
		name          string
		filteredCount int
		loadedCount   int
		wantLimit     int
		wantOffset    int
	}{
		{name: "default page size", filteredCount: 500, loadedCount: 0, wantLimit: eventsPageSize, wantOffset: 0},
		{name: "preserve loaded count larger than page", filteredCount: 5000, loadedCount: 4000, wantLimit: 4000, wantOffset: 1000},
		{name: "clamp small result set", filteredCount: 120, loadedCount: 20, wantLimit: eventsPageSize, wantOffset: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			limit, offset := currentRefreshEventWindow(tt.filteredCount, tt.loadedCount)
			if limit != tt.wantLimit || offset != tt.wantOffset {
				t.Fatalf("currentRefreshEventWindow() = (%d, %d), want (%d, %d)", limit, offset, tt.wantLimit, tt.wantOffset)
			}
		})
	}
}

func TestCurrentOlderEventsWindow(t *testing.T) {
	tests := []struct {
		name          string
		currentOffset int
		wantLimit     int
		wantOffset    int
	}{
		{name: "full older page", currentOffset: 6000, wantLimit: eventsPageSize, wantOffset: 3000},
		{name: "partial older page near top", currentOffset: 1200, wantLimit: 1200, wantOffset: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			limit, offset := currentOlderEventsWindow(tt.currentOffset)
			if limit != tt.wantLimit || offset != tt.wantOffset {
				t.Fatalf("currentOlderEventsWindow() = (%d, %d), want (%d, %d)", limit, offset, tt.wantLimit, tt.wantOffset)
			}
		})
	}
}
