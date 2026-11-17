package proxy

import (
	"errors"
	"io"
	"net/http"
	"sync"
	"time"
)

var bufPool = sync.Pool{
	New: func() any {
		// 8KB chunk buffer is standard for SSE chunks
		b := make([]byte, 8192)
		return &b
	},
}

// StreamSSE streams data from backend to client chunk-by-chunk without intermediate buffering.
// It measures TTFT (Time to First Token) and flushes immediately on every chunk.
func StreamSSE(w http.ResponseWriter, src io.Reader) (ttft time.Duration, bytesWritten int64, err error) {
	flusher, canFlush := w.(http.Flusher)
	bufPtr := bufPool.Get().(*[]byte)
	defer bufPool.Put(bufPtr)
	buf := *bufPtr

	start := time.Now()
	firstChunk := true

	for {
		n, readErr := src.Read(buf)
		if n > 0 {
			if firstChunk {
				ttft = time.Since(start)
				firstChunk = false
			}

			wn, writeErr := w.Write(buf[:n])
			bytesWritten += int64(wn)
			if writeErr != nil {
				return ttft, bytesWritten, writeErr
			}

			if canFlush {
				flusher.Flush()
			}
		}

		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return ttft, bytesWritten, nil
			}
			return ttft, bytesWritten, readErr
		}
	}
}
