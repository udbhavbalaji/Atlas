package audio

import (
	"encoding/binary"
	"math"
	"testing"
)

func TestPCMToWAVResamplesMonoAudio(t *testing.T) {
	const inputRate = 48000
	pcm := make([]byte, inputRate*2)
	for i := 0; i < inputRate; i++ {
		value := int16(10000 * math.Sin(2*math.Pi*440*float64(i)/inputRate))
		binary.LittleEndian.PutUint16(pcm[i*2:], uint16(value))
	}
	wav, err := PCMToWAV(pcm, inputRate)
	if err != nil {
		t.Fatal(err)
	}
	if string(wav[:4]) != "RIFF" || string(wav[8:16]) != "WAVEfmt " || string(wav[36:40]) != "data" {
		t.Fatal("invalid WAV header")
	}
	if got := binary.LittleEndian.Uint32(wav[24:28]); got != 16000 {
		t.Fatalf("sample rate = %d", got)
	}
	if len(wav) != 44+16000*2 {
		t.Fatalf("WAV length = %d", len(wav))
	}
	if rms(pcm) < 0.1 {
		t.Fatal("voiced signal was classified as silence")
	}
	if rms(make([]byte, inputRate*2)) != 0 {
		t.Fatal("silence was not silent")
	}
}

func TestPCMToWAVRejectsInvalidInput(t *testing.T) {
	for _, input := range []struct {
		pcm  []byte
		rate int
	}{{[]byte{1}, 16000}, {[]byte{0, 0}, 1}, {make([]byte, 31*16000*2), 16000}} {
		if _, err := PCMToWAV(input.pcm, input.rate); err == nil {
			t.Fatal("expected invalid PCM to fail")
		}
	}
}
