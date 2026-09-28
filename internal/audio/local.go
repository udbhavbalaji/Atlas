package audio

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Transcriber receives mono signed 16-bit PCM. Implementations return text
// only; audio and provisional text are never canonical Atlas records.
type Transcriber interface {
	Transcribe(context.Context, []byte, int) (string, error)
}

type Local struct {
	Backend string
	Binary  string
	Model   string
}

func Detect() Local {
	if runtime.GOOS == "linux" {
		binary, _ := exec.LookPath("voxtype")
		return Local{Backend: "voxtype", Binary: binary}
	}
	if runtime.GOOS == "darwin" {
		binary := os.Getenv("ATLAS_WHISPER_CLI")
		if binary == "" {
			candidates := []string{}
			if executable, err := os.Executable(); err == nil {
				candidates = append(candidates, filepath.Join(filepath.Dir(executable), "whisper-cli"))
			}
			candidates = append(candidates, "/opt/homebrew/bin/whisper-cli", "/usr/local/bin/whisper-cli")
			if path, err := exec.LookPath("whisper-cli"); err == nil {
				candidates = append(candidates, path)
			}
			for _, candidate := range candidates {
				if executableFile(candidate) {
					binary = candidate
					break
				}
			}
		}
		model := os.Getenv("ATLAS_WHISPER_MODEL")
		if model == "" {
			if home, err := os.UserHomeDir(); err == nil {
				model = filepath.Join(home, "Library", "Application Support", "Atlas", "models", "ggml-base.en.bin")
			}
		}
		return Local{Backend: "whisper-cli", Binary: binary, Model: model}
	}
	return Local{}
}

func (l Local) Available() bool {
	if !executableFile(l.Binary) {
		return false
	}
	if l.Backend == "whisper-cli" {
		_, err := os.Stat(l.Model)
		return err == nil
	}
	return l.Backend == "voxtype"
}

func executableFile(path string) bool {
	if path == "" {
		return false
	}
	stat, err := os.Stat(path)
	return err == nil && !stat.IsDir() && stat.Mode()&0111 != 0
}

func (l Local) MissingMessage() string {
	if l.Backend == "whisper-cli" {
		return "Install the local whisper-cli and base.en model as described in desktop/macos/README.md."
	}
	return "Install and configure Omarchy Voxtype for local transcription."
}

func (l Local) Transcribe(ctx context.Context, pcm []byte, sampleRate int) (string, error) {
	if !l.Available() {
		return "", errors.New(l.MissingMessage())
	}
	if len(pcm)%2 != 0 || sampleRate < 8000 || sampleRate > 96000 {
		return "", errors.New("invalid PCM stream")
	}
	if len(pcm) < sampleRate || rms(pcm) < 0.0015 {
		return "", nil
	}
	wav, err := PCMToWAV(pcm, sampleRate)
	if err != nil {
		return "", err
	}
	file, err := os.CreateTemp("", "atlas-voice-*.wav")
	if err != nil {
		return "", err
	}
	name := file.Name()
	defer os.Remove(name)
	if _, err = file.Write(wav); err != nil {
		file.Close()
		return "", err
	}
	if err = file.Close(); err != nil {
		return "", err
	}
	timeout, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	var command *exec.Cmd
	if l.Backend == "voxtype" {
		command = exec.CommandContext(timeout, l.Binary, "-q", "transcribe", name)
	} else {
		command = exec.CommandContext(timeout, l.Binary, "-np", "-nt", "-l", "en", "-m", l.Model, "-f", name)
	}
	output, err := command.Output()
	if err != nil {
		return "", fmt.Errorf("local transcription failed: %w", err)
	}
	text := strings.TrimSpace(string(output))
	if l.Backend == "voxtype" {
		if _, after, found := strings.Cut(text, "\n\n"); found {
			text = strings.TrimSpace(after)
		}
	}
	return text, nil
}

func rms(pcm []byte) float64 {
	if len(pcm) < 2 {
		return 0
	}
	var sum float64
	for i := 0; i+1 < len(pcm); i += 2 {
		value := float64(int16(binary.LittleEndian.Uint16(pcm[i:]))) / 32768
		sum += value * value
	}
	return math.Sqrt(sum / float64(len(pcm)/2))
}

// PCMToWAV resamples one mono PCM stream to Whisper's 16 kHz WAV input.
func PCMToWAV(pcm []byte, sampleRate int) ([]byte, error) {
	if len(pcm)%2 != 0 || sampleRate < 8000 || sampleRate > 96000 {
		return nil, errors.New("invalid PCM stream")
	}
	inputCount := len(pcm) / 2
	outputCount := int(float64(inputCount) * 16000 / float64(sampleRate))
	if outputCount > 30*16000 {
		return nil, errors.New("audio exceeds 30 seconds")
	}
	dataSize := outputCount * 2
	buffer := bytes.NewBuffer(make([]byte, 0, 44+dataSize))
	buffer.WriteString("RIFF")
	_ = binary.Write(buffer, binary.LittleEndian, uint32(36+dataSize))
	buffer.WriteString("WAVEfmt ")
	_ = binary.Write(buffer, binary.LittleEndian, uint32(16))
	_ = binary.Write(buffer, binary.LittleEndian, uint16(1))
	_ = binary.Write(buffer, binary.LittleEndian, uint16(1))
	_ = binary.Write(buffer, binary.LittleEndian, uint32(16000))
	_ = binary.Write(buffer, binary.LittleEndian, uint32(32000))
	_ = binary.Write(buffer, binary.LittleEndian, uint16(2))
	_ = binary.Write(buffer, binary.LittleEndian, uint16(16))
	buffer.WriteString("data")
	_ = binary.Write(buffer, binary.LittleEndian, uint32(dataSize))
	for i := 0; i < outputCount; i++ {
		position := float64(i) * float64(sampleRate) / 16000
		left := int(position)
		if left >= inputCount {
			left = inputCount - 1
		}
		right := left + 1
		if right >= inputCount {
			right = left
		}
		fraction := position - float64(left)
		a := float64(int16(binary.LittleEndian.Uint16(pcm[left*2:])))
		b := float64(int16(binary.LittleEndian.Uint16(pcm[right*2:])))
		value := int16(math.Round(a + (b-a)*fraction))
		_ = binary.Write(buffer, binary.LittleEndian, value)
	}
	return buffer.Bytes(), nil
}
