package ring

import (
	"sync"
	"time"
)

// RadixNode represents a node in the prefix tree containing prompt segments.
type RadixNode struct {
	Prefix   string
	Children []*RadixNode
	Workers  map[string]time.Time // WorkerID -> LastSeen
	HitCount int64
}

// RadixTree provides hierarchical prompt prefix matching for KV-cache reuse.
type RadixTree struct {
	mu           sync.RWMutex
	root         *RadixNode
	minMatchLen  int
	maxRetention time.Duration
}

// NewRadixTree initializes an empty prefix Radix Tree.
func NewRadixTree(minMatchLength int, maxRetention time.Duration) *RadixTree {
	if minMatchLength <= 0 {
		minMatchLength = 32 // default minimum 32 chars for common prefix
	}
	if maxRetention <= 0 {
		maxRetention = 1 * time.Hour
	}
	return &RadixTree{
		root: &RadixNode{
			Workers: make(map[string]time.Time),
		},
		minMatchLen:  minMatchLength,
		maxRetention: maxRetention,
	}
}

// Insert registers a prompt prefix as resident in a worker's KV-cache.
func (t *RadixTree) Insert(prefix string, workerID string) {
	if len(prefix) < t.minMatchLen || workerID == "" {
		return
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	curr := t.root
	remaining := prefix

	for len(remaining) > 0 {
		var matchedChild *RadixNode
		commonLen := 0

		for _, child := range curr.Children {
			cLen := commonPrefixLength(child.Prefix, remaining)
			if cLen > 0 {
				matchedChild = child
				commonLen = cLen
				break
			}
		}

		if matchedChild == nil {
			// No matching child, create new branch
			newChild := &RadixNode{
				Prefix:   remaining,
				Workers:  map[string]time.Time{workerID: time.Now()},
				HitCount: 1,
			}
			curr.Children = append(curr.Children, newChild)
			return
		}

		if commonLen < len(matchedChild.Prefix) {
			// Split the existing child node
			splitNode := &RadixNode{
				Prefix:   matchedChild.Prefix[commonLen:],
				Children: matchedChild.Children,
				Workers:  make(map[string]time.Time),
				HitCount: matchedChild.HitCount,
			}
			for w, seen := range matchedChild.Workers {
				splitNode.Workers[w] = seen
			}

			matchedChild.Prefix = matchedChild.Prefix[:commonLen]
			matchedChild.Children = []*RadixNode{splitNode}
			matchedChild.Workers[workerID] = time.Now()
			matchedChild.HitCount++

			remaining = remaining[commonLen:]
			if len(remaining) > 0 {
				leafNode := &RadixNode{
					Prefix:   remaining,
					Workers:  map[string]time.Time{workerID: time.Now()},
					HitCount: 1,
				}
				matchedChild.Children = append(matchedChild.Children, leafNode)
			}
			return
		}

		// Full match on child prefix, descend
		matchedChild.Workers[workerID] = time.Now()
		matchedChild.HitCount++
		curr = matchedChild
		remaining = remaining[commonLen:]
	}
}

// LongestPrefixMatch finds the worker with the deepest cached prefix for the given prompt.
func (t *RadixTree) LongestPrefixMatch(prompt string) (workerID string, matchedLength int) {
	if len(prompt) < t.minMatchLen {
		return "", 0
	}

	t.mu.RLock()
	defer t.mu.RUnlock()

	curr := t.root
	remaining := prompt
	totalMatched := 0
	var bestWorker string
	var bestMatchLen int
	var latestTime time.Time

	for len(remaining) > 0 {
		var matchedChild *RadixNode
		var matchLen int

		for _, child := range curr.Children {
			cLen := commonPrefixLength(child.Prefix, remaining)
			if cLen > 0 {
				matchedChild = child
				matchLen = cLen
				break
			}
		}

		if matchedChild == nil {
			break
		}

		totalMatched += matchLen

		for w, seen := range matchedChild.Workers {
			if seen.After(latestTime) {
				latestTime = seen
				bestWorker = w
				bestMatchLen = totalMatched
			}
		}

		if matchLen < len(matchedChild.Prefix) {
			// Partial match on child prefix, stop descending
			break
		}

		remaining = remaining[matchLen:]
		curr = matchedChild
	}

	if bestMatchLen >= t.minMatchLen {
		return bestWorker, bestMatchLen
	}
	return "", 0
}

func commonPrefixLength(a, b string) int {
	maxLen := len(a)
	if len(b) < maxLen {
		maxLen = len(b)
	}
	for i := 0; i < maxLen; i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return maxLen
}
