package telemetry

import (
	"testing"
	"time"

	"github.com/alexandrmotologa/kvring/internal/ring"
)

func TestMetricsCollectorRecordRoute(t *testing.T) {
	mc := NewMetricsCollector(50)

	w1 := ring.NewWorkerNode("worker-1", "http://127.0.0.1:8001", 1, 64)
	w2 := ring.NewWorkerNode("worker-2", "http://127.0.0.1:8002", 1, 64)

	decisionDirect := &ring.RouteDecision{
		PrimaryWorker:  w1,
		TargetWorker:   w1,
		Reason:         ring.ReasonDirectHit,
		SpilloverSteps: 0,
		RoutingLatency: 80 * time.Nanosecond,
	}

	mc.RecordRoute("session-1", decisionDirect, 200, 15*time.Millisecond, 80*time.Millisecond, true)

	events := mc.RecentEvents()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Spillover {
		t.Errorf("expected no spillover for direct hit")
	}

	// Test spillover event
	decisionSpill := &ring.RouteDecision{
		PrimaryWorker:  w1,
		TargetWorker:   w2,
		Reason:         ring.ReasonVramSaturationSpillover,
		SpilloverSteps: 1,
		RoutingLatency: 95 * time.Nanosecond,
	}
	mc.RecordRoute("session-2", decisionSpill, 200, 110*time.Millisecond, 250*time.Millisecond, false)

	events = mc.RecentEvents()
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	if !events[1].Spillover {
		t.Errorf("expected spillover true")
	}
	if events[1].Reason != string(ring.ReasonVramSaturationSpillover) {
		t.Errorf("expected reason vram_spillover, got %s", events[1].Reason)
	}
}
