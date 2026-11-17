package ring

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/alexandrmotologa/kvring/internal/config"
)

// AffinityExtractor extracts consistent routing keys from HTTP requests.
type AffinityExtractor struct {
	headerKey       string
	cookieKey       string
	enablePrefix    bool
	prefixLength    int
	fallbackHeaders []string
}

// NewAffinityExtractor creates an extractor based on AffinityConfig.
func NewAffinityExtractor(cfg config.AffinityConfig) *AffinityExtractor {
	return &AffinityExtractor{
		headerKey:       cfg.HeaderKey,
		cookieKey:       cfg.CookieKey,
		enablePrefix:    cfg.EnablePrefix,
		prefixLength:    cfg.PrefixLength,
		fallbackHeaders: cfg.FallbackHeaders,
	}
}

// openAIRequest is a minimal JSON schema for extracting prompts and sessions.
type openAIRequest struct {
	SessionID string `json:"session_id,omitempty"`
	User      string `json:"user,omitempty"`
	Model     string `json:"model,omitempty"`
	MaxTokens int    `json:"max_tokens,omitempty"`
	Prompt    any    `json:"prompt,omitempty"` // string or []string
	Messages  []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"messages,omitempty"`
}

// ExtractMetadata resolves the affinity key, target model, prompt text, and estimated token count.
func (e *AffinityExtractor) ExtractMetadata(req *http.Request) (affinityKey, model, prompt string, estimatedTokens int) {
	// 1. Direct custom header
	if e.headerKey != "" {
		if val := req.Header.Get(e.headerKey); val != "" {
			affinityKey = strings.TrimSpace(val)
		}
	}

	// 2. Query parameter
	if affinityKey == "" {
		if val := req.URL.Query().Get("session_id"); val != "" {
			affinityKey = strings.TrimSpace(val)
		}
	}

	// 3. Fallback headers
	if affinityKey == "" {
		for _, hdr := range e.fallbackHeaders {
			if val := req.Header.Get(hdr); val != "" {
				affinityKey = strings.TrimSpace(val)
				break
			}
		}
	}

	// 4. Cookie
	if affinityKey == "" && e.cookieKey != "" {
		if cookie, err := req.Cookie(e.cookieKey); err == nil && cookie.Value != "" {
			affinityKey = strings.TrimSpace(cookie.Value)
		}
	}

	// 5. JSON body inspection
	if req.Body != nil && strings.HasPrefix(req.Header.Get("Content-Type"), "application/json") {
		maxBody := int64(64 * 1024) // 64 KB prefix
		buf, err := io.ReadAll(io.LimitReader(req.Body, maxBody))
		if err == nil && len(buf) > 0 {
			req.Body = io.NopCloser(io.MultiReader(bytes.NewReader(buf), req.Body))

			var payload openAIRequest
			if jsonErr := json.Unmarshal(buf, &payload); jsonErr == nil {
				model = payload.Model

				// Calculate prompt string and approximate tokens
				prompt = e.extractPromptPrefix(&payload)
				tokenEst := len(prompt) / 4
				if payload.MaxTokens > 0 {
					tokenEst += payload.MaxTokens
				} else {
					tokenEst += 512 // typical default completion length
				}
				estimatedTokens = tokenEst

				if affinityKey == "" {
					if payload.SessionID != "" {
						affinityKey = payload.SessionID
					} else if payload.User != "" {
						affinityKey = payload.User
					} else if e.enablePrefix && prompt != "" {
						affinityKey = fmt.Sprintf("prefix:%08x", HashKey(prompt))
					}
				}
			}
		}
	}

	// 6. Client RemoteAddr fallback
	if affinityKey == "" && req.RemoteAddr != "" {
		parts := strings.Split(req.RemoteAddr, ":")
		if len(parts) > 0 && parts[0] != "" {
			affinityKey = parts[0]
		}
	}

	// 7. Ephemeral random session key
	if affinityKey == "" {
		b := make([]byte, 8)
		_, _ = rand.Read(b)
		affinityKey = hex.EncodeToString(b)
	}

	return affinityKey, model, prompt, estimatedTokens
}

// Extract resolves the affinity key for an incoming request.
func (e *AffinityExtractor) Extract(req *http.Request) string {
	key, _, _, _ := e.ExtractMetadata(req)
	return key
}

func (e *AffinityExtractor) extractPromptPrefix(req *openAIRequest) string {
	// From messages (chat completions)
	if len(req.Messages) > 0 {
		var sb strings.Builder
		for i, msg := range req.Messages {
			if i > 2 {
				break // Take at most first 2 messages (e.g. system + initial user turn)
			}
			sb.WriteString(msg.Role)
			sb.WriteString(":")
			sb.WriteString(msg.Content)
			sb.WriteString("\n")
		}
		res := sb.String()
		if len(res) > e.prefixLength && e.prefixLength > 0 {
			return res[:e.prefixLength]
		}
		return res
	}

	// From prompt string
	if strPrompt, ok := req.Prompt.(string); ok && strPrompt != "" {
		if len(strPrompt) > e.prefixLength && e.prefixLength > 0 {
			return strPrompt[:e.prefixLength]
		}
		return strPrompt
	}

	return ""
}
