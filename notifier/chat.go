package notifier

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/izerok/frigate-notify-plus/config"
	"github.com/izerok/frigate-notify-plus/util"
)

// chatMaxToolIterations caps the Frigate tool-call rounds a chat completion
// may run (event search, recaps, ...).
const chatMaxToolIterations = 5

// chatTimeout bounds one chat completion. A completion can run several
// LLM + tool rounds, so it gets a deadline independent of the global HTTP
// timeout.
const chatTimeout = 180 * time.Second

// frigateChat asks Frigate's GenAI chat (with tool calling) a question and
// returns the assistant's final answer. It reuses the GenAI provider
// configured in Frigate, so frigate-notify needs no LLM settings of its
// own.
func frigateChat(prompt string) (string, error) {
	url := config.ConfigData.Frigate.Server + "/api/chat/completion"

	payload := map[string]any{
		"messages": []map[string]string{
			{"role": "user", "content": prompt},
		},
		"max_tool_iterations": chatMaxToolIterations,
		"stream":              false,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("failed to marshal chat request: %w", err)
	}

	log.Debug().Str("url", url).Msg("Requesting summary from Frigate GenAI chat")

	// Go through the shared Frigate HTTP path (cookie jar + login session,
	// ignoressl, configured headers) like every other Frigate call; the
	// endpoint requires an authenticated session when Frigate auth is on.
	respBody, statusCode, err := util.HTTPPostFrigate(url, config.ConfigData.Frigate.Insecure,
		body, "application/json", chatTimeout, config.ConfigData.Frigate.Headers...)
	if err != nil {
		return "", fmt.Errorf("Frigate chat request failed: %w", err)
	}

	var result struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
		Error        string `json:"error"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("failed to parse chat response: %w", err)
	}
	if statusCode < 200 || statusCode >= 300 || result.Error != "" {
		reason := result.Error
		if reason == "" {
			reason = string(respBody)
			if len(reason) > 300 {
				reason = reason[:300] + "…"
			}
		}
		return "", fmt.Errorf("Frigate chat failed (status %d): %s", statusCode, reason)
	}
	// When the model burns its tool-call budget, Frigate still answers 200
	// but the content is a canned "I reached the maximum number of tool
	// call iterations..." line (finish_reason "length"), not an answer.
	// Treat it as a failure so the caller falls back instead of notifying
	// the canned line as the digest.
	if result.FinishReason == "length" {
		return "", fmt.Errorf("Frigate chat exhausted its tool-call iterations (finish_reason=length)")
	}
	content := strings.TrimSpace(result.Message.Content)
	if content == "" {
		return "", fmt.Errorf("Frigate chat returned an empty answer")
	}
	return content, nil
}
