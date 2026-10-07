package tui

import "testing"

func TestPaneDefaultsToMain(t *testing.T) {
	s := New(Options{Model: "some/model"})

	if got := s.Pane(); got != "main" {
		t.Errorf("Pane() = %q, want main", got)
	}
}

func TestSetPaneAcceptsEachKnownName(t *testing.T) {
	s := New(Options{Model: "some/model"})

	for _, name := range []string{"delegate", "spawn", "main"} {
		if err := s.SetPane(name); err != nil {
			t.Fatalf("SetPane(%q): %v", name, err)
		}
		if got := s.Pane(); got != name {
			t.Errorf("Pane() = %q, want %q", got, name)
		}
	}
}

func TestSetPaneRefusesAnUnknownName(t *testing.T) {
	s := New(Options{Model: "some/model"})

	if err := s.SetPane("sidebar"); err == nil {
		t.Fatal("SetPane(\"sidebar\") was accepted")
	}
	if got := s.Pane(); got != "main" {
		t.Errorf("Pane() after a refused SetPane = %q, want it unchanged", got)
	}
}

func TestQueueIsFIFO(t *testing.T) {
	s := New(Options{Model: "some/model"})

	s.Enqueue("first")
	s.Enqueue("second")

	if got := s.QueueLen(); got != 2 {
		t.Fatalf("QueueLen() = %d, want 2", got)
	}

	first, ok := s.Drain()
	if !ok || first != "first" {
		t.Errorf("first Drain() = %q, %v", first, ok)
	}

	second, ok := s.Drain()
	if !ok || second != "second" {
		t.Errorf("second Drain() = %q, %v", second, ok)
	}

	if got := s.QueueLen(); got != 0 {
		t.Errorf("QueueLen() after draining = %d, want 0", got)
	}
}

func TestDrainOfAnEmptyQueueReportsNothingWaiting(t *testing.T) {
	s := New(Options{Model: "some/model"})

	if _, ok := s.Drain(); ok {
		t.Error("Drain() of an empty queue reported something waiting")
	}
}
