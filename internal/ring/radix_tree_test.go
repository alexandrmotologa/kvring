package ring

import (
	"testing"
)

func TestRadixTreeInsertAndMatch(t *testing.T) {
	tree := NewRadixTree(16, 0)

	systemPromptA := "System instruction: You are a specialized security audit agent. Verify all SQL queries."
	userTurn1 := systemPromptA + " Turn 1: Analyze user input: SELECT * FROM users;"
	userTurn2 := systemPromptA + " Turn 2: Now analyze: DROP TABLE products;"

	// Insert Turn 1 on worker-1
	tree.Insert(userTurn1, "worker-1")

	// Match Turn 2 - should match systemPromptA on worker-1
	matchedWorker, matchedLen := tree.LongestPrefixMatch(userTurn2)
	if matchedWorker != "worker-1" {
		t.Fatalf("expected match on worker-1, got: %s", matchedWorker)
	}

	if matchedLen < len(systemPromptA) {
		t.Errorf("expected matched length at least %d, got %d", len(systemPromptA), matchedLen)
	}

	// Completely different prompt should yield no match
	otherPrompt := "Completely unrelated prompt about cooking pasta."
	otherWorker, otherLen := tree.LongestPrefixMatch(otherPrompt)
	if otherWorker != "" || otherLen != 0 {
		t.Errorf("expected no match, got worker: %s, len: %d", otherWorker, otherLen)
	}
}

func BenchmarkRadixTreeMatch(b *testing.B) {
	tree := NewRadixTree(16, 0)
	systemPrompt := "System instruction: You are an enterprise code reviewer specialized in Go and Rust."
	tree.Insert(systemPrompt+" Context A: func main() {}", "worker-1")
	queryPrompt := systemPrompt + " Context B: func Benchmark() {}"

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _ = tree.LongestPrefixMatch(queryPrompt)
	}
}
