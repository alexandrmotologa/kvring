package proxy

import (
	"context"
	"fmt"
	"net"
	"sync"

	"github.com/alexandrmotologa/kvring/internal/ring"
)

// GRPCProxy coordinates gRPC stream routing to backend inference nodes.
type GRPCProxy struct {
	router *ring.Router
	ring   *ring.HashRing
	mu     sync.RWMutex
	conns  map[string]net.Conn
}

// NewGRPCProxy creates a new gRPC inference routing coordinator.
func NewGRPCProxy(r *ring.Router, hr *ring.HashRing) *GRPCProxy {
	return &GRPCProxy{
		router: r,
		ring:   hr,
		conns:  make(map[string]net.Conn),
	}
}

// RouteStream resolves the optimal backend worker for a gRPC inference stream.
func (g *GRPCProxy) RouteStream(ctx context.Context, sessionID string) (*ring.WorkerNode, error) {
	decision, err := g.router.Route(sessionID)
	if err != nil {
		return nil, fmt.Errorf("route grpc stream: %w", err)
	}
	return decision.TargetWorker, nil
}
