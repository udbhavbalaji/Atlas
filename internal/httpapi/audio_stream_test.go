package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

type fixtureTranscriber struct {
	mu    sync.Mutex
	calls int
}

func (f *fixtureTranscriber) Transcribe(_ context.Context, pcm []byte, rate int) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if rate != 8000 || len(pcm) == 0 {
		return "", context.Canceled
	}
	return "recognized words", nil
}

func TestAudioStreamPartialAndFinal(t *testing.T) {
	fixture := &fixtureTranscriber{}
	mux := http.NewServeMux()
	audioStreamRoutesWithService(mux, &audioStreamService{transcriber: fixture, backend: "fixture", available: true, modelSlot: make(chan struct{}, 1)})
	server := httptest.NewServer(mux)
	defer server.Close()
	response, err := http.Get(server.URL + "/api/v1/audio/capabilities")
	if err != nil || response.StatusCode != 200 {
		t.Fatal(err, response)
	}
	response.Body.Close()
	connection, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/api/v1/audio/stream", http.Header{"Origin": {server.URL}})
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	_ = connection.SetReadDeadline(time.Now().Add(10 * time.Second))
	if err := connection.WriteJSON(map[string]any{"type": "start", "sample_rate": 8000}); err != nil {
		t.Fatal(err)
	}
	var message struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := connection.ReadJSON(&message); err != nil || message.Type != "ready" {
		t.Fatal(err, message)
	}
	frame := make([]byte, 8000*2)
	for i := 0; i < len(frame); i += 2 {
		frame[i+1] = 16
	}
	for i := 0; i < 2; i++ {
		if err := connection.WriteMessage(websocket.BinaryMessage, frame); err != nil {
			t.Fatal(err)
		}
	}
	if err := connection.ReadJSON(&message); err != nil || message.Type != "partial" || message.Text != "recognized words" {
		t.Fatal(err, message)
	}
	for update := 0; update < 2; update++ {
		for i := 0; i < 3; i++ {
			if err := connection.WriteMessage(websocket.BinaryMessage, frame); err != nil {
				t.Fatal(err)
			}
		}
		if err := connection.ReadJSON(&message); err != nil || message.Type != "partial" || message.Text != "recognized words" {
			t.Fatal(err, message)
		}
	}
	if err := connection.WriteJSON(map[string]string{"type": "stop"}); err != nil {
		t.Fatal(err)
	}
	partials := 3
	for {
		if err := connection.ReadJSON(&message); err != nil {
			t.Fatal(err)
		}
		if message.Type == "partial" {
			partials++
			continue
		}
		if message.Type != "final" || message.Text != "recognized words" {
			t.Fatal(message)
		}
		break
	}
	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	if fixture.calls != partials+1 || partials < 3 {
		t.Fatalf("transcription calls = %d, partials = %d", fixture.calls, partials)
	}
}

func TestAudioStreamRejectsForeignOrigin(t *testing.T) {
	mux := http.NewServeMux()
	audioStreamRoutesWithService(mux, &audioStreamService{available: true, modelSlot: make(chan struct{}, 1)})
	server := httptest.NewServer(mux)
	defer server.Close()
	_, response, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/api/v1/audio/stream", http.Header{"Origin": {"http://example.invalid"}})
	if err == nil || response == nil || response.StatusCode != http.StatusForbidden {
		t.Fatal("foreign origin was accepted", err)
	}
}
