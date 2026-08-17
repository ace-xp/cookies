package creative

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math"
)

const brandAudioFixtureSampleRate = 48000

// RenderBrandAudioFixtureWAV creates deterministic, audible development media.
// It is deliberately synthetic and must remain disclosed as Fixture rather
// than being presented as generated speech or licensed production music.
func RenderBrandAudioFixtureWAV(trackType, seed string, durationMS int) ([]byte, []float64, error) {
	if durationMS < 100 || durationMS > 60_000 {
		return nil, nil, fmt.Errorf("fixture audio duration is invalid")
	}
	sampleCount := brandAudioFixtureSampleRate * durationMS / 1000
	samples := make([]int16, sampleCount)
	digest := sha256.Sum256([]byte(trackType + "\x00" + seed))
	baseFrequency := 180.0 + float64(int(digest[0])%180)
	state := uint32(digest[1])<<24 | uint32(digest[2])<<16 | uint32(digest[3])<<8 | uint32(digest[4])
	for index := range samples {
		t := float64(index) / brandAudioFixtureSampleRate
		progress := float64(index) / float64(max(1, sampleCount-1))
		envelope := math.Min(1, progress*20) * math.Min(1, (1-progress)*20)
		var value float64
		switch trackType {
		case BrandAudioTrackMusic:
			value = 0.22*math.Sin(2*math.Pi*baseFrequency*t) + 0.12*math.Sin(2*math.Pi*baseFrequency*1.5*t)
		case BrandAudioTrackAmbience:
			state = state*1664525 + 1013904223
			noise := float64(int32(state)) / float64(math.MaxInt32)
			value = 0.08*math.Sin(2*math.Pi*(baseFrequency*.18)*t) + noise*0.05
		case BrandAudioTrackSFX:
			state = state*1664525 + 1013904223
			noise := float64(int32(state)) / float64(math.MaxInt32)
			value = noise * 0.42 * math.Exp(-7*progress)
		default:
			phrasePulse := 0.55 + 0.45*math.Sin(2*math.Pi*3.1*t)
			value = 0.28 * math.Sin(2*math.Pi*baseFrequency*t) * phrasePulse
		}
		samples[index] = int16(math.Max(-1, math.Min(1, value*envelope)) * math.MaxInt16)
	}
	peaks := waveformPeaks(samples, 64)
	dataSize := len(samples) * 2
	var output bytes.Buffer
	output.WriteString("RIFF")
	_ = binary.Write(&output, binary.LittleEndian, uint32(36+dataSize))
	output.WriteString("WAVEfmt ")
	_ = binary.Write(&output, binary.LittleEndian, uint32(16))
	_ = binary.Write(&output, binary.LittleEndian, uint16(1))
	_ = binary.Write(&output, binary.LittleEndian, uint16(1))
	_ = binary.Write(&output, binary.LittleEndian, uint32(brandAudioFixtureSampleRate))
	_ = binary.Write(&output, binary.LittleEndian, uint32(brandAudioFixtureSampleRate*2))
	_ = binary.Write(&output, binary.LittleEndian, uint16(2))
	_ = binary.Write(&output, binary.LittleEndian, uint16(16))
	output.WriteString("data")
	_ = binary.Write(&output, binary.LittleEndian, uint32(dataSize))
	for _, sample := range samples {
		_ = binary.Write(&output, binary.LittleEndian, sample)
	}
	return output.Bytes(), peaks, nil
}

func waveformPeaks(samples []int16, buckets int) []float64 {
	if len(samples) == 0 || buckets < 1 {
		return []float64{}
	}
	if buckets > len(samples) {
		buckets = len(samples)
	}
	peaks := make([]float64, buckets)
	for index, sample := range samples {
		bucket := index * buckets / len(samples)
		value := math.Abs(float64(sample) / math.MaxInt16)
		if value > peaks[bucket] {
			peaks[bucket] = math.Round(value*1000) / 1000
		}
	}
	return peaks
}
