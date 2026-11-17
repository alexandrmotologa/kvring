package tui

import (
	"strings"
	"testing"

	"github.com/alexandrmotologa/kvring/internal/ring"
	"github.com/alexandrmotologa/kvring/internal/telemetry"
	tea "github.com/charmbracelet/bubbletea"
)

func TestTUIModelAndRender(t *testing.T) {
	hr := ring.NewHashRing(100)
	w1 := ring.NewWorkerNode("gpu-node-1", "http://127.0.0.1:8001", 1, 64)
	w2 := ring.NewWorkerNode("gpu-node-2", "http://127.0.0.1:8002", 1, 64)
	hr.AddWorker(w1)
	hr.AddWorker(w2)

	router := ring.NewRouter(hr, 0.25, 0.85)
	tm := telemetry.NewMetricsCollector(10)

	model := NewModel(router, hr, tm)

	// Check initial view renders cleanly
	viewStr := model.View()
	if !strings.Contains(viewStr, "gpu-node-1") {
		t.Errorf("expected view to contain gpu-node-1, got: %s", viewStr)
	}

	// Test navigation down
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyDown})
	m := updated.(Model)
	if m.selectedIdx != 1 {
		t.Errorf("expected selectedIdx to be 1 after KeyDown, got %d", m.selectedIdx)
	}

	// Test kill key 'k'
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	if w2.Healthy.Load() {
		t.Errorf("expected gpu-node-2 to be marked unhealthy after 'k' key")
	}

	// Test recover key 'r'
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	if !w2.Healthy.Load() {
		t.Errorf("expected gpu-node-2 to be restored to healthy after 'r' key")
	}
}
