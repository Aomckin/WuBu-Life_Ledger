package ai

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAnalyzeInventoryCommandUsesStructuredResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/chat" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		var request chatRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if request.Model != "qwen3:4b-instruct" || request.Stream || request.Format == nil || request.Options.Temperature != 0 {
			t.Fatalf("request = %#v", request)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"message": map[string]any{
				"content": `{"summary":"Review one use.","suggestions":[{"intent":"decrease","item_name":"Dish soap","amount":1,"unit":"bottle","category":"cleaning","confidence":0.96}]}`,
			},
		})
	}))
	defer server.Close()

	client, err := New(Config{BaseURL: server.URL, Model: "qwen3:4b-instruct", Timeout: time.Second})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	analysis, err := client.AnalyzeInventoryCommand(t.Context(), "Used one bottle of dish soap")
	if err != nil {
		t.Fatalf("AnalyzeInventoryCommand() error = %v", err)
	}
	if !analysis.NeedsReview || len(analysis.Suggestions) != 1 || analysis.Suggestions[0].Intent != "decrease" || analysis.Suggestions[0].Category != "cleaning" {
		t.Fatalf("analysis = %#v", analysis)
	}
}

func TestAnalyzeInventoryCommandSplitsLongShoppingList(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_ = json.NewEncoder(w).Encode(map[string]any{"message": map[string]any{"content": `{"summary":"Part processed.","suggestions":[{"intent":"increase","item_name":"Item","amount":1,"unit":"pcs","category":"pantry","confidence":0.9}]}`}})
	}))
	defer server.Close()
	client, err := New(Config{BaseURL: server.URL, Model: "qwen3:4b-instruct", Timeout: time.Second})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	list := strings.Repeat("One shopping item, ", 700)
	analysis, err := client.AnalyzeInventoryCommand(t.Context(), list)
	if err != nil || calls < 2 || len(analysis.Suggestions) != calls || !analysis.NeedsReview {
		t.Fatalf("chunked analysis = %#v, calls=%d, err=%v", analysis, calls, err)
	}
}

func TestNewRequiresCompleteConfiguration(t *testing.T) {
	if _, err := New(Config{Model: "qwen3:4b-instruct", Timeout: time.Second}); err == nil {
		t.Fatal("New() error = nil")
	}
	if _, err := New(Config{BaseURL: "http://example.test", Timeout: time.Second}); err == nil {
		t.Fatal("New() error = nil")
	}
}

func TestAnalyzeInventoryIntakeSendsVisualInput(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request chatRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if len(request.Messages) != 2 || len(request.Messages[1].Images) != 1 || request.Messages[1].Images[0] == "" {
			t.Fatalf("visual request = %#v", request)
		}
		if !strings.Contains(request.Messages[0].Content, "every distinct visible physical product") || !strings.Contains(request.Messages[0].Content, "Never return only the most legible") {
			t.Fatalf("visual inventory instruction missing: %q", request.Messages[0].Content)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"message": map[string]any{
				"content": `{"summary":"Review receipt.","suggestions":[]}`,
			},
		})
	}))
	defer server.Close()

	client, err := New(Config{BaseURL: server.URL, Model: "qwen3-vl:8b-instruct", Timeout: time.Second})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	analysis, err := client.AnalyzeInventoryIntake(t.Context(), IntakeInput{
		Source: "receipt", FileName: "receipt.jpg", MediaType: "image/jpeg", Images: [][]byte{[]byte("image")},
	})
	if err != nil || analysis.Summary != "Review receipt." || !analysis.NeedsReview {
		t.Fatalf("AnalyzeInventoryIntake() = %#v, %v", analysis, err)
	}
}

func TestAnalyzeInventoryIntakeRetriesWithOneImage(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request chatRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		calls++
		switch calls {
		case 1:
			if got := len(request.Messages[1].Images); got != 2 {
				t.Fatalf("first image count = %d", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"message": map[string]any{"content": `{"summary":`}})
		case 2:
			if got := len(request.Messages[1].Images); got != 1 {
				t.Fatalf("retry image count = %d", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"message": map[string]any{"content": `{"summary":"Review video.","suggestions":[]}`}})
		default:
			t.Fatalf("unexpected call %d", calls)
		}
	}))
	defer server.Close()

	client, err := New(Config{BaseURL: server.URL, Model: "qwen3-vl:8b-instruct", Timeout: time.Second})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	analysis, err := client.AnalyzeInventoryIntake(t.Context(), IntakeInput{
		Source: "receipt", Images: [][]byte{[]byte("first"), []byte("second")},
	})
	if err != nil || calls != 2 || analysis.Summary != "Review video." || !analysis.NeedsReview {
		t.Fatalf("AnalyzeInventoryIntake() = %#v, calls=%d, err=%v", analysis, calls, err)
	}
}

func TestAnalyzeInventoryIntakeUsesManualReviewForInvalidCompletion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"message": map[string]any{"content": `{"summary":`}})
	}))
	defer server.Close()

	client, err := New(Config{BaseURL: server.URL, Model: "qwen3-vl:8b-instruct", Timeout: time.Second})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	analysis, err := client.AnalyzeInventoryIntake(t.Context(), IntakeInput{
		Source: "receipt", Images: [][]byte{[]byte("image")},
	})
	if err != nil || analysis.Summary != "Local AI response needs manual review." || len(analysis.Suggestions) != 0 || !analysis.NeedsReview {
		t.Fatalf("AnalyzeInventoryIntake() = %#v, %v", analysis, err)
	}
}

func TestTranscribeUsesMultipartEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1024); err != nil {
			t.Fatalf("parse multipart form: %v", err)
		}
		if got := r.FormValue("language"); got != "de" {
			t.Fatalf("language = %q", got)
		}
		file, _, err := r.FormFile("file")
		if err != nil {
			t.Fatalf("read audio file: %v", err)
		}
		defer file.Close()
		data := new(bytes.Buffer)
		if _, err := data.ReadFrom(file); err != nil || data.String() != "audio" {
			t.Fatalf("audio = %q, %v", data.String(), err)
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"text": "Eine Flasche Seife verbraucht."})
	}))
	defer server.Close()

	client, err := New(Config{
		BaseURL: server.URL, Model: "qwen3-vl:8b-instruct", Timeout: time.Second,
		TranscriptionURL: server.URL + "/inference", TranscriptionLanguage: "de",
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	transcript, err := client.Transcribe(t.Context(), AudioInput{FileName: "voice.webm", MediaType: "audio/webm", Data: []byte("audio")})
	if err != nil || transcript != "Eine Flasche Seife verbraucht." {
		t.Fatalf("Transcribe() = %q, %v", transcript, err)
	}
}
