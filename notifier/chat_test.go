package notifier

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/izerok/frigate-notify-plus/config"
	"github.com/izerok/frigate-notify-plus/models"
)

const benignReviewsJSON = `[
	{"camera":"room","start_time":1700000100,"data":{"metadata":{"title":"有人进入房间","potential_threat_level":0,"other_concerns":null}}},
	{"camera":"room","start_time":1700000200,"data":{"metadata":{"title":"有人在厨房做饭","potential_threat_level":0,"other_concerns":null}}}
]`

// mockFrigate serves benign reviews on /api/review, Frigate's canned
// placeholder on /api/review/summarize, and — when chatOK — an assistant
// reply on /api/chat/completion (capturing the prompt sent).
func mockFrigate(t *testing.T, summarizeCalled *bool, chatPrompt *string, chatOK bool) *httptest.Server {
	t.Helper()
	frigate := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/api/chat/completion"):
			if !chatOK {
				w.WriteHeader(http.StatusInternalServerError)
				w.Write([]byte(`{"error":"An error occurred while processing your request."}`))
				return
			}
			body, _ := io.ReadAll(r.Body)
			var req struct {
				Messages []struct {
					Role    string `json:"role"`
					Content string `json:"content"`
				} `json:"messages"`
			}
			_ = json.Unmarshal(body, &req)
			for _, m := range req.Messages {
				if m.Role == "user" {
					*chatPrompt = m.Content
				}
			}
			w.Write([]byte(`{"message":{"role":"assistant","content":"今天家中正常：早晨有人进入房间，随后在厨房做饭。"},"finish_reason":"stop","tool_iterations":0,"tool_calls":[],"messages":[]}`))
		case strings.HasPrefix(r.URL.Path, "/api/review/summarize"):
			*summarizeCalled = true
			w.Write([]byte(`{"success":true,"summary":"No concerns were found during this time period."}`))
		default:
			w.Write([]byte(benignReviewsJSON))
		}
	}))
	t.Cleanup(frigate.Close)
	return frigate
}

// TestFrigateChat exercises the chat completion call: request shape and
// response parsing, including Frigate's error body.
func TestFrigateChat(t *testing.T) {
	var gotMessages []map[string]string
	var gotMaxToolIterations int
	var gotStream bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Messages          []map[string]string `json:"messages"`
			MaxToolIterations int                 `json:"max_tool_iterations"`
			Stream            bool                `json:"stream"`
		}
		_ = json.Unmarshal(body, &req)
		gotMessages = req.Messages
		gotMaxToolIterations = req.MaxToolIterations
		gotStream = req.Stream
		w.Write([]byte(`{"message":{"role":"assistant","content":"  今天一切正常。 "}}`))
	}))
	t.Cleanup(srv.Close)

	origConfig := config.ConfigData
	t.Cleanup(func() { config.ConfigData = origConfig })
	config.ConfigData = config.Config{Frigate: models.Frigate{Server: srv.URL}}

	summary, err := frigateChat("总结今天")
	if err != nil {
		t.Fatalf("frigateChat returned error: %v", err)
	}
	if summary != "今天一切正常。" {
		t.Errorf("summary = %q, want the trimmed assistant content", summary)
	}
	if len(gotMessages) != 1 || gotMessages[0]["role"] != "user" || gotMessages[0]["content"] != "总结今天" {
		t.Errorf("messages = %+v, want a single user message with the prompt", gotMessages)
	}
	if gotMaxToolIterations != chatMaxToolIterations {
		t.Errorf("max_tool_iterations = %d, want %d", gotMaxToolIterations, chatMaxToolIterations)
	}
	if gotStream {
		t.Error("stream = true, want false (a streamed NDJSON response would not parse)")
	}
}

// TestFrigateChatError: Frigate's generic error body surfaces as an error
// so the caller can fall back.
func TestFrigateChatError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		w.Write([]byte(`{"error":"An error occurred while processing your request."}`))
	}))
	t.Cleanup(srv.Close)

	origConfig := config.ConfigData
	t.Cleanup(func() { config.ConfigData = origConfig })
	config.ConfigData = config.Config{Frigate: models.Frigate{Server: srv.URL}}

	_, err := frigateChat("总结今天")
	if err == nil || !strings.Contains(err.Error(), "An error occurred") {
		t.Errorf("error = %v, want it to contain the Frigate error", err)
	}
}

// TestRunGenAIScheduleChatDigest: the schedule triggers Frigate's GenAI
// chat with the (default) digest prompt plus the per-event list, and the
// assistant's answer is what gets notified. A configured schedule prompt
// replaces the default.
func TestRunGenAIScheduleChatDigest(t *testing.T) {
	summarizeCalled := false
	var chatPrompt string
	frigate := mockFrigate(t, &summarizeCalled, &chatPrompt, true)
	posts := setupScheduleTest(t, frigate.URL)
	p := runScheduleAndCollect(t, posts)

	if summarizeCalled {
		t.Error("Frigate summarize called when the chat answered")
	}
	if p.GenAI == nil || !strings.Contains(p.GenAI.Summary, "今天家中正常") {
		t.Errorf("webhook summary = %+v, want the chat answer", p.GenAI)
	}
	if !strings.Contains(chatPrompt, "有人进入房间") || !strings.Contains(chatPrompt, "有人在厨房做饭") {
		t.Errorf("chat prompt = %q, want it to contain the per-event list", chatPrompt)
	}

	// A configured prompt replaces the default.
	config.ConfigData.Alerts.General.GenAI.Schedules = []models.GenAISchedule{
		{Enabled: models.BoolPtr(true), Time: "20:00", Window: "today", Prompt: "自定义：只说有没有异常。"},
	}
	chatPrompt = ""
	runScheduleAndCollect(t, posts)
	if !strings.Contains(chatPrompt, "自定义：只说有没有异常。") {
		t.Errorf("chat prompt = %q, want it to start with the configured prompt", chatPrompt)
	}
}

// TestRunGenAIScheduleChatFailure: when the chat call fails the schedule
// falls back to the per-event list instead of dropping the digest.
func TestRunGenAIScheduleChatFailure(t *testing.T) {
	summarizeCalled := false
	var chatPrompt string
	frigate := mockFrigate(t, &summarizeCalled, &chatPrompt, false)
	posts := setupScheduleTest(t, frigate.URL)
	p := runScheduleAndCollect(t, posts)

	if p.GenAI == nil || !strings.Contains(p.GenAI.Summary, "有人进入房间") || !strings.Contains(p.GenAI.Summary, "有人在厨房做饭") {
		t.Errorf("webhook summary = %+v, want the per-event list fallback", p.GenAI)
	}
}

// TestFrigateChatInsecureTLS pins the TLS leg of the Frigate HTTP
// contract: against a self-signed HTTPS Frigate, the chat call succeeds
// only when ignoressl is set — a bare client without the shared
// transport config fails every run with an x509 error.
func TestFrigateChatInsecureTLS(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"message":{"role":"assistant","content":"一切正常。"},"finish_reason":"stop"}`))
	}))
	t.Cleanup(srv.Close)

	origConfig := config.ConfigData
	t.Cleanup(func() { config.ConfigData = origConfig })

	config.ConfigData = config.Config{Frigate: models.Frigate{Server: srv.URL, Insecure: true}}
	summary, err := frigateChat("总结今天")
	if err != nil {
		t.Fatalf("frigateChat with ignoressl returned error: %v", err)
	}
	if summary != "一切正常。" {
		t.Errorf("summary = %q, want the assistant content", summary)
	}

	config.ConfigData = config.Config{Frigate: models.Frigate{Server: srv.URL, Insecure: false}}
	if _, err := frigateChat("总结今天"); err == nil {
		t.Error("frigateChat without ignoressl against a self-signed server should fail TLS verification")
	}
}

// TestFrigateChatFinishReasonLength: when Frigate's model exhausts its
// tool-call iterations it answers 200 with finish_reason "length" and a
// canned English non-answer as the content. That must surface as an
// error, not as a successful summary.
func TestFrigateChatFinishReasonLength(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"message":{"role":"assistant","content":"I reached the maximum number of tool call iterations. Please try rephrasing your question."},"finish_reason":"length","tool_iterations":5,"tool_calls":[],"messages":[]}`))
	}))
	t.Cleanup(srv.Close)

	origConfig := config.ConfigData
	t.Cleanup(func() { config.ConfigData = origConfig })
	config.ConfigData = config.Config{Frigate: models.Frigate{Server: srv.URL}}

	_, err := frigateChat("总结今天")
	if err == nil || !strings.Contains(err.Error(), "finish_reason=length") {
		t.Errorf("error = %v, want a finish_reason=length error", err)
	}
}

// TestRunGenAIScheduleChatLengthFallback: a schedule whose chat run ends
// with finish_reason "length" falls back to the per-event list; the
// canned iteration line is never notified as the digest.
func TestRunGenAIScheduleChatLengthFallback(t *testing.T) {
	summarizeCalled := false
	frigate := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/api/chat/completion"):
			w.Write([]byte(`{"message":{"role":"assistant","content":"I reached the maximum number of tool call iterations. Please try rephrasing your question."},"finish_reason":"length","tool_iterations":5}`))
		case strings.HasPrefix(r.URL.Path, "/api/review/summarize"):
			summarizeCalled = true
			w.Write([]byte(`{"success":true,"summary":"No concerns were found during this time period."}`))
		default:
			w.Write([]byte(benignReviewsJSON))
		}
	}))
	t.Cleanup(frigate.Close)
	posts := setupScheduleTest(t, frigate.URL)
	p := runScheduleAndCollect(t, posts)

	if summarizeCalled {
		t.Error("Frigate summarize called for a benign window after a length-limited chat")
	}
	if p.GenAI == nil {
		t.Fatal("webhook post has no genai data")
	}
	if strings.Contains(p.GenAI.Summary, "maximum number of tool call iterations") {
		t.Errorf("webhook summary = %q, the canned iteration line must not be the digest", p.GenAI.Summary)
	}
	if !strings.Contains(p.GenAI.Summary, "有人进入房间") || !strings.Contains(p.GenAI.Summary, "有人在厨房做饭") {
		t.Errorf("webhook summary = %q, want the per-event list fallback", p.GenAI.Summary)
	}
}
