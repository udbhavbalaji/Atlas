package httpapi

import (
	"atlas/internal/audio"
	"bytes"
	"encoding/json"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const audioMaxSeconds = 30

type audioStreamService struct {
	transcriber audio.Transcriber
	backend     string
	available   bool
	missing     string
	modelSlot   chan struct{}
}

func audioStreamRoutes(mux *http.ServeMux) {
	local := audio.Detect()
	audioStreamRoutesWithService(mux, &audioStreamService{
		transcriber: local, backend: local.Backend, available: local.Available(),
		missing: local.MissingMessage(), modelSlot: make(chan struct{}, 1),
	})
}

func audioStreamRoutesWithService(mux *http.ServeMux, service *audioStreamService) {
	mux.HandleFunc("GET /api/v1/audio/capabilities", func(w http.ResponseWriter, r *http.Request) {
		respond(w, 200, map[string]any{
			"streaming": true, "local_transcription": service.available,
			"backend": service.backend, "max_seconds": audioMaxSeconds,
			"partial_interval_seconds": 3,
		})
	})
	mux.HandleFunc("GET /api/v1/audio/stream", func(w http.ResponseWriter, r *http.Request) {
		peer, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil || net.ParseIP(peer) == nil || !net.ParseIP(peer).IsLoopback() || r.Header.Get("Origin") != "http://"+r.Host {
			http.Error(w, "Local same-origin audio only", http.StatusForbidden)
			return
		}
		upgrader := websocket.Upgrader{CheckOrigin: func(request *http.Request) bool {
			return request.Header.Get("Origin") == "http://"+request.Host
		}}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		conn.SetReadLimit(64 * 1024)
		_ = conn.SetReadDeadline(time.Now().Add(45 * time.Second))
		var writeMu sync.Mutex
		send := func(kind, message string) {
			writeMu.Lock()
			defer writeMu.Unlock()
			_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			_ = conn.WriteJSON(map[string]string{"type": kind, "text": message})
		}
		if !service.available {
			send("error", service.missing)
			return
		}
		kind, body, err := conn.ReadMessage()
		if err != nil || kind != websocket.TextMessage {
			send("error", "Audio stream must begin with a sample rate.")
			return
		}
		var start struct {
			Type       string `json:"type"`
			SampleRate int    `json:"sample_rate"`
		}
		if json.Unmarshal(body, &start) != nil || start.Type != "start" || start.SampleRate < 8000 || start.SampleRate > 96000 {
			send("error", "Invalid audio sample rate.")
			return
		}
		send("ready", "")
		var pcm bytes.Buffer
		nextPartialBytes := 2 * start.SampleRate * 2
		var partialDone chan struct{}
		for {
			kind, body, err = conn.ReadMessage()
			if err != nil {
				return
			}
			if kind == websocket.TextMessage {
				var stop struct {
					Type string `json:"type"`
				}
				if json.Unmarshal(body, &stop) != nil || stop.Type != "stop" {
					send("error", "Unexpected audio control message.")
					return
				}
				if partialDone != nil {
					<-partialDone
				}
				service.modelSlot <- struct{}{}
				text, transcribeErr := service.transcriber.Transcribe(r.Context(), pcm.Bytes(), start.SampleRate)
				<-service.modelSlot
				if transcribeErr != nil {
					send("error", "Local transcription failed: "+transcribeErr.Error())
				} else {
					send("final", text)
				}
				return
			}
			if kind != websocket.BinaryMessage || len(body)%2 != 0 || len(body) == 0 {
				send("error", "Invalid PCM audio frame.")
				return
			}
			if pcm.Len()+len(body) > start.SampleRate*audioMaxSeconds*2 {
				send("error", "Recording reached the 30 second limit.")
				return
			}
			_, _ = pcm.Write(body)
			if partialDone != nil {
				select {
				case <-partialDone:
					partialDone = nil
				default:
				}
			}
			if partialDone == nil && pcm.Len() >= nextPartialBytes {
				select {
				case service.modelSlot <- struct{}{}:
					copyPCM := bytes.Clone(pcm.Bytes())
					partialDone = make(chan struct{})
					nextPartialBytes = pcm.Len() + 3*start.SampleRate*2
					go func(done chan struct{}) {
						defer close(done)
						defer func() { <-service.modelSlot }()
						text, err := service.transcriber.Transcribe(r.Context(), copyPCM, start.SampleRate)
						if err == nil {
							send("partial", text)
						}
					}(partialDone)
				default:
				}
			}
		}
	})
}
