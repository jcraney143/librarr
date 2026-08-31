package webhook

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestBuildDiscordEmbed_Completed(t *testing.T) {
	p := Payload{
		Event:     EventDownloadComplete,
		Title:     "Dune",
		Message:   "Download finished successfully",
		Status:    "completed",
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}

	msg := buildDiscordEmbed(p)

	if len(msg.Embeds) != 1 {
		t.Fatalf("expected 1 embed, got %d", len(msg.Embeds))
	}

	embed := msg.Embeds[0]
	if embed.Title != "Dune" {
		t.Errorf("expected title Dune, got %s", embed.Title)
	}
	if embed.Description != "Download finished successfully" {
		t.Errorf("expected description, got %s", embed.Description)
	}
	if embed.Color != 3066993 { // green
		t.Errorf("expected green color 3066993, got %d", embed.Color)
	}
	if embed.Footer == nil || embed.Footer.Text != "Librarr" {
		t.Error("expected Librarr footer")
	}
}

func TestBuildDiscordEmbed_Failed(t *testing.T) {
	p := Payload{
		Event:   EventDownloadFailed,
		Title:   "Failed Book",
		Message: "Error occurred",
		Status:  "failed",
	}

	msg := buildDiscordEmbed(p)
	if msg.Embeds[0].Color != 15158332 { // red
		t.Errorf("expected red color 15158332, got %d", msg.Embeds[0].Color)
	}
}

func TestBuildDiscordEmbed_Info(t *testing.T) {
	p := Payload{
		Event:   EventInfo,
		Title:   "Info",
		Message: "Something happened",
		Status:  "info",
	}

	msg := buildDiscordEmbed(p)
	if msg.Embeds[0].Color != 3447003 { // blue
		t.Errorf("expected blue color 3447003, got %d", msg.Embeds[0].Color)
	}
}

func TestFormatGenericPayload(t *testing.T) {
	p := Payload{
		Event:     EventDownloadComplete,
		Title:     "Test Book",
		Message:   "Done",
		Status:    "completed",
		Timestamp: "2026-01-01T00:00:00Z",
		Extra:     map[string]interface{}{"author": "Test Author"},
	}

	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var decoded map[string]interface{}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if decoded["event"] != "download_complete" {
		t.Errorf("expected event download_complete, got %v", decoded["event"])
	}
	if decoded["title"] != "Test Book" {
		t.Errorf("expected title Test Book, got %v", decoded["title"])
	}
	if decoded["status"] != "completed" {
		t.Errorf("expected status completed, got %v", decoded["status"])
	}

	extra, ok := decoded["extra"].(map[string]interface{})
	if !ok {
		t.Fatal("expected extra map")
	}
	if extra["author"] != "Test Author" {
		t.Errorf("expected extra author Test Author, got %v", extra["author"])
	}
}

func TestSendWebhook_Generic(t *testing.T) {
	var receivedBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected application/json, got %s", r.Header.Get("Content-Type"))
		}
		if r.Header.Get("User-Agent") != "Librarr/2.0" {
			t.Errorf("expected User-Agent Librarr/2.0, got %s", r.Header.Get("User-Agent"))
		}
		json.NewDecoder(r.Body).Decode(&receivedBody)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	sender := NewSender()
	err := sender.send(Config{
		URL:     server.URL,
		Type:    "generic",
		Enabled: true,
		Events:  "*",
	}, Payload{
		Event:     EventDownloadComplete,
		Title:     "Test",
		Message:   "Completed",
		Status:    "completed",
		Timestamp: "2026-01-01T00:00:00Z",
	})

	if err != nil {
		t.Fatalf("send failed: %v", err)
	}
	if receivedBody["title"] != "Test" {
		t.Errorf("expected title Test, got %v", receivedBody["title"])
	}
}

func TestSendWebhook_Discord(t *testing.T) {
	var receivedBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&receivedBody)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	sender := NewSender()
	err := sender.send(Config{
		URL:     server.URL,
		Type:    "discord",
		Enabled: true,
		Events:  "*",
	}, Payload{
		Event:     EventDownloadComplete,
		Title:     "Discord Test",
		Message:   "Done",
		Status:    "completed",
		Timestamp: "2026-01-01T00:00:00Z",
	})

	if err != nil {
		t.Fatalf("send failed: %v", err)
	}

	embeds, ok := receivedBody["embeds"].([]interface{})
	if !ok || len(embeds) == 0 {
		t.Fatal("expected embeds array")
	}
	embed := embeds[0].(map[string]interface{})
	if embed["title"] != "Discord Test" {
		t.Errorf("expected title Discord Test, got %v", embed["title"])
	}
}

func TestSendWebhook_ErrorStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	sender := NewSender()
	err := sender.send(Config{
		URL:     server.URL,
		Type:    "generic",
		Enabled: true,
		Events:  "*",
	}, Payload{
		Event: EventInfo,
		Title: "Test",
	})

	if err == nil {
		t.Error("expected error for 500 status")
	}
}

func TestSendAsync(t *testing.T) {
	var mu sync.Mutex
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		callCount++
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	sender := NewSender()
	sender.SetConfigs([]Config{
		{ID: 1, Name: "test", URL: server.URL, Type: "generic", Enabled: true, Events: "*"},
	})

	// Send should not block.
	sender.Send(Payload{
		Event:   EventDownloadComplete,
		Title:   "Async Test",
		Message: "Should not block",
		Status:  "completed",
	})

	// Give the goroutine time to complete.
	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if callCount != 1 {
		t.Errorf("expected 1 call, got %d", callCount)
	}
}

func TestSender_SetAndGetConfigs(t *testing.T) {
	sender := NewSender()

	configs := []Config{
		{ID: 1, Name: "test1", URL: "http://example.com/1", Type: "generic", Enabled: true, Events: "*"},
		{ID: 2, Name: "test2", URL: "http://example.com/2", Type: "discord", Enabled: false, Events: "download_complete"},
	}
	sender.SetConfigs(configs)

	got := sender.GetConfigs()
	if len(got) != 2 {
		t.Fatalf("expected 2 configs, got %d", len(got))
	}
	if got[0].Name != "test1" {
		t.Errorf("expected test1, got %s", got[0].Name)
	}
	if got[1].Type != "discord" {
		t.Errorf("expected discord, got %s", got[1].Type)
	}
}

func TestEventMatches(t *testing.T) {
	tests := []struct {
		events   string
		event    string
		expected bool
	}{
		{"download_complete,download_failed", "download_complete", true},
		{"download_complete,download_failed", "download_failed", true},
		{"download_complete,download_failed", "info", false},
		{"download_complete", "download_complete", true},
		{"download_complete", "download_failed", false},
		{"", "download_complete", false},
	}

	for _, tt := range tests {
		result := eventMatches(tt.events, tt.event)
		if result != tt.expected {
			t.Errorf("eventMatches(%q, %q) = %v, want %v", tt.events, tt.event, result, tt.expected)
		}
	}
}

func TestSplitEvents(t *testing.T) {
	tests := []struct {
		input    string
		expected int
	}{
		{"download_complete,download_failed,info", 3},
		{"download_complete", 1},
		{"", 0},
		{" download_complete , download_failed ", 2},
	}

	for _, tt := range tests {
		result := splitEvents(tt.input)
		if len(result) != tt.expected {
			t.Errorf("splitEvents(%q) returned %d items, want %d", tt.input, len(result), tt.expected)
		}
	}
}

func TestSend_SkipsDisabledConfigs(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	sender := NewSender()
	sender.SetConfigs([]Config{
		{ID: 1, Name: "disabled", URL: server.URL, Type: "generic", Enabled: false, Events: "*"},
	})

	sender.Send(Payload{Event: EventInfo, Title: "Test"})
	time.Sleep(100 * time.Millisecond)

	if callCount != 0 {
		t.Errorf("expected 0 calls for disabled config, got %d", callCount)
	}
}

func TestSend_FiltersEvents(t *testing.T) {
	var mu sync.Mutex
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		callCount++
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	sender := NewSender()
	sender.SetConfigs([]Config{
		{ID: 1, Name: "filtered", URL: server.URL, Type: "generic", Enabled: true, Events: "download_complete"},
	})

	// Send event that doesn't match.
	sender.Send(Payload{Event: EventInfo, Title: "Test"})
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	if callCount != 0 {
		t.Errorf("expected 0 calls for non-matching event, got %d", callCount)
	}
	mu.Unlock()

	// Send event that matches.
	sender.Send(Payload{Event: EventDownloadComplete, Title: "Test"})
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	if callCount != 1 {
		t.Errorf("expected 1 call for matching event, got %d", callCount)
	}
	mu.Unlock()
}

func TestTest_SendsTestPayload(t *testing.T) {
	var receivedBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&receivedBody)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	sender := NewSender()
	err := sender.Test(Config{URL: server.URL, Type: "generic"})
	if err != nil {
		t.Fatalf("Test failed: %v", err)
	}

	if receivedBody["title"] != "Librarr Webhook Test" {
		t.Errorf("expected test title, got %v", receivedBody["title"])
	}
	if receivedBody["status"] != "info" {
		t.Errorf("expected info status, got %v", receivedBody["status"])
	}
}
