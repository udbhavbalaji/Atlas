package httpapi

import (
	"atlas/internal/audio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os/exec"
	"runtime"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const audioMaxSeconds = 30

type audioStreamService struct {
	transcriber   audio.Transcriber
	backend       string
	available     bool
	missing       string
	modelSlot     chan struct{}
	captureBinary string
}

func audioStreamRoutes(mux *http.ServeMux) {
	local := audio.Detect()
	captureBinary := ""
	if runtime.GOOS == "linux" {
		captureBinary, _ = exec.LookPath("parec")
	}
	audioStreamRoutesWithService(mux, &audioStreamService{
		transcriber: local, backend: local.Backend, available: local.Available(),
		missing: local.MissingMessage(), modelSlot: make(chan struct{}, 1), captureBinary: captureBinary,
	})
}

func audioStreamRoutesWithService(mux *http.ServeMux, service *audioStreamService) {
	mux.HandleFunc("GET /api/v1/audio/capabilities", func(w http.ResponseWriter, r *http.Request) {
		respond(w, 200, map[string]any{
			"streaming": true, "local_transcription": service.available,
			"backend": service.backend, "max_seconds": audioMaxSeconds,
			"partial_interval_seconds": 3,
			"server_capture":           service.available && service.captureBinary != "",
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
			Source     string `json:"source"`
		}
		if json.Unmarshal(body, &start) != nil || start.Type != "start" || start.SampleRate < 8000 || start.SampleRate > 96000 {
			send("error", "Invalid audio sample rate.")
			return
		}
		if start.Source == "server" {
			if service.captureBinary == "" || start.SampleRate != 16000 {
				send("error", "Local microphone capture is unavailable.")
				return
			}
			streamServerMicrophone(r.Context(), conn, service, send)
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

// On Linux the local server can capture from PulseAudio/PipeWire directly.
// This keeps streaming transcription available when WebKit denies user media.
func streamServerMicrophone(ctx context.Context, conn *websocket.Conn, service *audioStreamService, send func(string, string)) {
	captureCtx, stopCapture := context.WithCancel(ctx)
	defer stopCapture()
	command := exec.CommandContext(captureCtx, service.captureBinary, "--raw", "--format=s16le", "--rate=16000", "--channels=1")
	stdout, err := command.StdoutPipe()
	if err != nil || command.Start() != nil {
		send("error", "Could not open the default microphone.")
		return
	}
	defer command.Wait()
	send("ready", "")
	var pcm bytes.Buffer
	partialDone := make(chan struct{}, 1)
	readDone := make(chan error, 1)
	go func() {
		frame := make([]byte, 4096)
		nextPartialBytes := 2 * 16000 * 2
		for {
			n, readErr := stdout.Read(frame)
			if n > 0 {
				if pcm.Len()+n > 16000*audioMaxSeconds*2 {
					readDone <- nil
					return
				}
				_, _ = pcm.Write(frame[:n])
				if pcm.Len() >= nextPartialBytes {
					select {
					case service.modelSlot <- struct{}{}:
						copyPCM := bytes.Clone(pcm.Bytes())
						nextPartialBytes = pcm.Len() + 3*16000*2
						partialDone <- struct{}{}
						go func() {
							defer func() { <-service.modelSlot; <-partialDone }()
							text, transcribeErr := service.transcriber.Transcribe(ctx, copyPCM, 16000)
							if transcribeErr == nil && text != "" {
								send("partial", text)
							}
						}()
					default:
					}
				}
			}
			if readErr != nil {
				if captureCtx.Err() == nil {
					send("error", "The default microphone stopped sending audio. Check its input device and try again.")
				}
				readDone <- readErr
				return
			}
		}
	}()
	_ = conn.SetReadDeadline(time.Now().Add(31 * time.Second))
	kind, body, readErr := conn.ReadMessage()
	if readErr == nil {
		var stop struct {
			Type string `json:"type"`
		}
		if kind != websocket.TextMessage || json.Unmarshal(body, &stop) != nil || stop.Type != "stop" {
			send("error", "Unexpected audio control message.")
			return
		}
	}
	stopCapture()
	captureErr := <-readDone
	if readErr != nil {
		return
	}
	if captureErr != nil && captureErr != io.EOF && pcm.Len() == 0 {
		send("error", "Microphone capture stopped before audio arrived.")
		return
	}
	service.modelSlot <- struct{}{}
	text, transcribeErr := service.transcriber.Transcribe(ctx, pcm.Bytes(), 16000)
	<-service.modelSlot
	if transcribeErr != nil {
		send("error", "Local transcription failed: "+transcribeErr.Error())
	} else {
		send("final", text)
	}
}
