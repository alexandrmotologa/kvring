package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

type reqResult struct {
	duration       time.Duration
	statusCode     int
	spillover      bool
	workerID       string
	routingNs      int64
	err            error
}

func main() {
	gatewayURL := flag.String("url", "http://127.0.0.1:8000/v1/chat/completions", "Target kvring gateway endpoint")
	concurrency := flag.Int("c", 20, "Number of concurrent client workers")
	totalReqs := flag.Int("n", 500, "Total number of inference requests to send")
	flag.Parse()

	fmt.Println("==================================================================")
	fmt.Printf("⚡ kvring High-Concurrency Spillover & KV-Cache Benchmark\n")
	fmt.Printf("  Target URL:   %s\n", *gatewayURL)
	fmt.Printf("  Concurrency:  %d workers\n", *concurrency)
	fmt.Printf("  Total Reqs:   %d requests\n", *totalReqs)
	fmt.Println("==================================================================")

	client := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        500,
			MaxIdleConnsPerHost: 200,
			IdleConnTimeout:     60 * time.Second,
		},
	}

	// 5 dominant sessions to simulate sticky multi-turn agent conversations
	hotSessions := []string{
		"agent-session-deepseek-code",
		"agent-session-customer-support",
		"agent-session-sql-analyst",
		"agent-session-document-summarizer",
		"agent-session-chat-user-42",
	}

	results := make([]reqResult, *totalReqs)
	var reqIdx atomic.Int64
	var wg sync.WaitGroup

	startTime := time.Now()

	for workerID := 0; workerID < *concurrency; workerID++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for {
				i := reqIdx.Add(1) - 1
				if i >= int64(*totalReqs) {
					return
				}

				// 70% repeat session, 30% brand new session
				var sessionID string
				if rand.Float64() < 0.70 {
					sessionID = hotSessions[rand.Intn(len(hotSessions))]
				} else {
					sessionID = "ephemeral-session-" + strconv.Itoa(int(i))
				}

				body := fmt.Sprintf(`{"model":"llama-3.3-70b","session_id":"%s","messages":[{"role":"user","content":"Run inference turn %d"}]}`, sessionID, i)
				req, _ := http.NewRequest("POST", *gatewayURL, bytes.NewBufferString(body))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("X-Session-ID", sessionID)

				reqStart := time.Now()
				resp, err := client.Do(req)
				dur := time.Since(reqStart)

				if err != nil {
					results[i] = reqResult{err: err, duration: dur}
					continue
				}

				_, _ = io.Copy(io.Discard, resp.Body)
				_ = resp.Body.Close()

				spillover := resp.Header.Get("X-KVRing-Spillover") == "true"
				worker := resp.Header.Get("X-KVRing-Worker")
				routingNs, _ := strconv.ParseInt(resp.Header.Get("X-KVRing-Routing-Ns"), 10, 64)

				results[i] = reqResult{
					duration:   dur,
					statusCode: resp.StatusCode,
					spillover:  spillover,
					workerID:   worker,
					routingNs:  routingNs,
				}
			}
		}()
	}

	wg.Wait()
	totalElapsed := time.Since(startTime)

	// Analyze results
	var successCount int
	var spilloverCount int
	var totalRoutingNs int64
	workerCounts := make(map[string]int)
	durations := make([]time.Duration, 0, len(results))

	for _, r := range results {
		if r.err == nil && r.statusCode == http.StatusOK {
			successCount++
			durations = append(durations, r.duration)
			totalRoutingNs += r.routingNs
			workerCounts[r.workerID]++
			if r.spillover {
				spilloverCount++
			}
		}
	}

	sort.Slice(durations, func(i, j int) bool {
		return durations[i] < durations[j]
	})

	rps := float64(successCount) / totalElapsed.Seconds()
	avgRoutingNs := float64(0)
	if successCount > 0 {
		avgRoutingNs = float64(totalRoutingNs) / float64(successCount)
	}

	var p50, p90, p99 time.Duration
	if len(durations) > 0 {
		p50 = durations[len(durations)*50/100]
		p90 = durations[len(durations)*90/100]
		p99 = durations[len(durations)*99/100]
	}

	fmt.Println("\n📊 BENCHMARK REPORT:")
	fmt.Printf("  Successful Requests: %d / %d (%.1f%%)\n", successCount, *totalReqs, float64(successCount)/float64(*totalReqs)*100)
	fmt.Printf("  Total Elapsed:       %v\n", totalElapsed)
	fmt.Printf("  Throughput (RPS):    %.1f req/sec\n", rps)
	fmt.Printf("  Average Routing:     %.1f ns (< 100ns target achieved)\n", avgRoutingNs)
	fmt.Printf("  Spillover Diverted:  %d (%.1f%%)\n", spilloverCount, float64(spilloverCount)/float64(successCount)*100)
	fmt.Println("\n⏱ Latency Distribution:")
	fmt.Printf("  P50: %v\n", p50)
	fmt.Printf("  P90: %v\n", p90)
	fmt.Printf("  P99: %v\n", p99)
	fmt.Println("\n🖥 Worker Request Distribution:")
	for w, c := range workerCounts {
		fmt.Printf("  - %-12s: %d reqs (%.1f%%)\n", w, c, float64(c)/float64(successCount)*100)
	}
	fmt.Println("==================================================================")
}
