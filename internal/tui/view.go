package tui

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
)

var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FAFAFA")).
			Background(lipgloss.Color("#7D56F4")).
			Padding(0, 1)

	cardStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#7D56F4")).
			Padding(0, 1)

	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#04B575"))

	selectedRowStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#FFFFFF")).
				Background(lipgloss.Color("#5A56E0"))

	healthyBadge = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#04B575")).
			Bold(true).
			Render("● HEALTHY")

	ejectedBadge = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FF4D4D")).
			Bold(true).
			Render("✖ EJECTED")

	footerStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#888888")).
			Italic(true)
)

// View renders the terminal dashboard.
func (m Model) View() string {
	if m.quitting {
		return "kvring TUI shut down cleanly.\n"
	}

	workers := m.ring.Workers()
	healthyCount := m.ring.HealthyWorkerCount()
	totalLoad := m.ring.ClusterTotalLoad()
	capacityCeiling := m.router.ComputeCapacity(totalLoad, healthyCount)

	// 1. Header Banner
	banner := titleStyle.Render("⚡ kvring: KV-Cache Affinity & Mirrokni Bounded-Load Router")
	statsLine := fmt.Sprintf("Cluster Load: %d in-flight | Healthy Nodes: %d/%d | Mirrokni Bound (C): %d | Ring Replicas: %d",
		totalLoad, healthyCount, len(workers), capacityCeiling, m.ring.TotalVirtualNodes())

	header := lipgloss.JoinVertical(lipgloss.Left, banner, statsLine)

	// 2. Workers Table
	var tableRows []string
	tableRows = append(tableRows, headerStyle.Render(fmt.Sprintf("%-2s %-12s %-22s %-12s %-18s %-8s %-8s %-8s",
		"", "WORKER ID", "ENDPOINT", "STATUS", "VRAM / KV-CACHE", "IN-FLIGHT", "SPILL-IN", "TOTAL")))

	for i, w := range workers {
		prefix := "  "
		if i == m.selectedIdx {
			prefix = "▶ "
		}

		statusStr := healthyBadge
		if !w.Healthy.Load() {
			statusStr = ejectedBadge
		}

		vramRatio := w.GetVramRatio()
		vramBar := renderBar(vramRatio, 10)
		vramStr := fmt.Sprintf("[%s] %3.0f%%", vramBar, vramRatio*100)

		row := fmt.Sprintf("%-2s %-12s %-22s %-12s %-18s %-8d %-8d %-8d",
			prefix,
			w.ID,
			w.URL,
			statusStr,
			vramStr,
			w.ActiveLoad.Load(),
			w.SpilloversIn.Load(),
			w.TotalRequests.Load(),
		)

		if i == m.selectedIdx {
			tableRows = append(tableRows, selectedRowStyle.Render(row))
		} else {
			tableRows = append(tableRows, row)
		}
	}

	workerTableBox := cardStyle.Render(lipgloss.JoinVertical(lipgloss.Left, tableRows...))

	// 3. Live Routing Stream Log
	var logRows []string
	logRows = append(logRows, headerStyle.Render("LIVE ROUTING & SPILLOVER STREAM"))
	if len(m.eventLog) == 0 {
		logRows = append(logRows, "  Waiting for incoming inference requests...")
	} else {
		start := 0
		if len(m.eventLog) > 8 {
			start = len(m.eventLog) - 8
		}
		for _, evt := range m.eventLog[start:] {
			timeStr := evt.Timestamp.Format("15:04:05.000")
			spillLabel := "DIRECT"
			if evt.Spillover {
				spillLabel = fmt.Sprintf("SPILL(%s->%s)", evt.PrimaryWorker, evt.TargetWorker)
			}
			logLine := fmt.Sprintf("  [%s] %-18s -> %-10s | %-20s | TTFT: %5.1fms | Total: %5.1fms",
				timeStr,
				truncate(evt.AffinityKey, 18),
				evt.TargetWorker,
				spillLabel,
				evt.TTFT,
				evt.TotalDuration,
			)
			logRows = append(logRows, logLine)
		}
	}
	logBox := cardStyle.Render(lipgloss.JoinVertical(lipgloss.Left, logRows...))

	// 4. Hotkeys Footer
	footer := footerStyle.Render("Keys: [↑/↓] Navigate  [k] Kill Node  [r] Recover Node  [t] Inject Traffic Burst  [q] Quit")

	return lipgloss.JoinVertical(lipgloss.Left,
		header,
		"\n",
		workerTableBox,
		"\n",
		logBox,
		"\n",
		footer,
	)
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	if max <= 3 {
		return s[:max]
	}
	return s[:max-3] + "..."
}
