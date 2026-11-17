package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/alexandrmotologa/kvring/internal/ring"
	"github.com/alexandrmotologa/kvring/internal/telemetry"
	tea "github.com/charmbracelet/bubbletea"
)

// TickMsg triggers periodic UI refresh.
type TickMsg time.Time

// Model manages the Bubble Tea TUI state.
type Model struct {
	router      *ring.Router
	ring        *ring.HashRing
	telemetry   *telemetry.MetricsCollector
	selectedIdx int
	quitting    bool
	eventLog    []telemetry.RouteEvent
	width       int
	height      int
	eventChan   chan telemetry.RouteEvent
}

// NewModel creates an initialized TUI model.
func NewModel(r *ring.Router, hr *ring.HashRing, tm *telemetry.MetricsCollector) Model {
	m := Model{
		router:      r,
		ring:        hr,
		telemetry:   tm,
		selectedIdx: 0,
		width:       100,
		height:      30,
		eventChan:   tm.Subscribe(),
	}
	m.eventLog = tm.RecentEvents()
	return m
}

// Init starts the periodic ticker and listens for events.
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		tea.Tick(250*time.Millisecond, func(t time.Time) tea.Msg {
			return TickMsg(t)
		}),
		waitForEvent(m.eventChan),
	)
}

func waitForEvent(ch chan telemetry.RouteEvent) tea.Cmd {
	return func() tea.Msg {
		if ch == nil {
			return nil
		}
		evt, ok := <-ch
		if !ok {
			return nil
		}
		return evt
	}
}

// Update handles key presses, window resizes, and event ticks.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			m.quitting = true
			if m.eventChan != nil {
				m.telemetry.Unsubscribe(m.eventChan)
			}
			return m, tea.Quit

		case "up", "k_nav":
			if m.selectedIdx > 0 {
				m.selectedIdx--
			}

		case "down", "j_nav":
			workers := m.ring.Workers()
			if m.selectedIdx < len(workers)-1 {
				m.selectedIdx++
			}

		case "k": // Kill/Fail selected node
			workers := m.ring.Workers()
			if len(workers) > 0 && m.selectedIdx < len(workers) {
				target := workers[m.selectedIdx]
				m.ring.SetWorkerHealth(target.ID, false)
			}

		case "r": // Recover selected node
			workers := m.ring.Workers()
			if len(workers) > 0 && m.selectedIdx < len(workers) {
				target := workers[m.selectedIdx]
				m.ring.SetWorkerHealth(target.ID, true)
			}

		case "t": // Trigger simulated traffic spike
			go func() {
				for i := 0; i < 20; i++ {
					k := fmt.Sprintf("burst-session-%d", i%5)
					_, _ = m.router.Route(k)
				}
			}()
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case telemetry.RouteEvent:
		m.eventLog = append(m.eventLog, msg)
		if len(m.eventLog) > 30 {
			m.eventLog = m.eventLog[1:]
		}
		return m, waitForEvent(m.eventChan)

	case TickMsg:
		m.eventLog = m.telemetry.RecentEvents()
		return m, tea.Tick(250*time.Millisecond, func(t time.Time) tea.Msg {
			return TickMsg(t)
		})
	}

	return m, nil
}

// Helper for rendering horizontal progress bar
func renderBar(ratio float64, width int) string {
	if ratio < 0 {
		ratio = 0
	} else if ratio > 1 {
		ratio = 1
	}
	filled := int(ratio * float64(width))
	if filled > width {
		filled = width
	}
	empty := width - filled

	return strings.Repeat("█", filled) + strings.Repeat("░", empty)
}
