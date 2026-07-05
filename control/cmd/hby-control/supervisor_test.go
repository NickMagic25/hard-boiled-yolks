package main

import (
	"testing"
	"time"
)

func TestLogHubClearClearsSnapshotAndNotifiesSubscribers(t *testing.T) {
	hub := newLogHub(1024)
	ch, unsubscribe := hub.Subscribe()
	defer unsubscribe()

	hub.Append([]byte("old logs"))
	event := nextLogEvent(t, ch)
	if event.Type != "output" || string(event.Data) != "old logs" {
		t.Fatalf("unexpected append event: %#v", event)
	}

	hub.Clear()
	event = nextLogEvent(t, ch)
	if event.Type != "clear" || len(event.Data) != 0 {
		t.Fatalf("unexpected clear event: %#v", event)
	}
	if snapshot := hub.Snapshot(); len(snapshot) != 0 {
		t.Fatalf("expected clear snapshot, got %q", string(snapshot))
	}

	hub.Append([]byte("new logs"))
	event = nextLogEvent(t, ch)
	if event.Type != "output" || string(event.Data) != "new logs" {
		t.Fatalf("unexpected post-clear append event: %#v", event)
	}
	if snapshot := hub.Snapshot(); string(snapshot) != "new logs" {
		t.Fatalf("unexpected post-clear snapshot: %q", string(snapshot))
	}
}

func nextLogEvent(t *testing.T, ch <-chan logEvent) logEvent {
	t.Helper()
	select {
	case event := <-ch:
		return event
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for log event")
		return logEvent{}
	}
}
