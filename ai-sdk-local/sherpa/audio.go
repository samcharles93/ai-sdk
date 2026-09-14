package sherpa

import (
	"bytes"
	"encoding/binary"
	"math"
)

// pcm16 converts normalised float samples to mono int16 little-endian bytes,
// clamping values outside [-1, 1].
func pcm16(samples []float32) []byte {
	out := make([]byte, len(samples)*2)
	for i, sample := range samples {
		v := math.Max(-1, math.Min(1, float64(sample)))
		binary.LittleEndian.PutUint16(out[i*2:], uint16(int16(math.Round(v*math.MaxInt16))))
	}
	return out
}

// encodeWAV wraps mono int16 samples in a RIFF/WAVE container.
func encodeWAV(samples []float32, sampleRate int) []byte {
	data := pcm16(samples)
	buf := bytes.NewBuffer(make([]byte, 0, 44+len(data)))

	write := func(v any) { _ = binary.Write(buf, binary.LittleEndian, v) }

	buf.WriteString("RIFF")
	write(uint32(36 + len(data)))
	buf.WriteString("WAVE")
	buf.WriteString("fmt ")
	write(uint32(16))             // PCM chunk size
	write(uint16(1))              // format: PCM
	write(uint16(1))              // channels: mono
	write(uint32(sampleRate))     // sample rate
	write(uint32(sampleRate * 2)) // byte rate
	write(uint16(2))              // block align
	write(uint16(16))             // bits per sample
	buf.WriteString("data")
	write(uint32(len(data)))
	buf.Write(data)
	return buf.Bytes()
}
