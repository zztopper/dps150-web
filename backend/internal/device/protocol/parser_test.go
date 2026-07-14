package protocol

import (
	"bytes"
	"reflect"
	"testing"
)

// frameEqual compares two frames by value.
func frameEqual(a, b Frame) bool {
	return a.Group == b.Group && a.Reg == b.Reg && bytes.Equal(a.Data, b.Data)
}

func TestParserGarbageBeforeFrame(t *testing.T) {
	var p Parser
	stream := append(mustHex(t, "00 13 37 AB"), mustHex(t, "F0 A1 DB 01 01 DD")...)

	frames := p.Feed(stream)

	if len(frames) != 1 {
		t.Fatalf("got %d frames, want 1", len(frames))
	}
	want := Frame{Group: GroupRead, Reg: RegOutputEnable, Data: []byte{0x01}}
	if !frameEqual(frames[0], want) {
		t.Errorf("frame = %+v, want %+v", frames[0], want)
	}
}

func TestParserByteByByte(t *testing.T) {
	var p Parser
	stream := mustHex(t, "F0 A1 C1 04 CD CC 44 41 E3") // voltage set echo, 12.3 V

	var frames []Frame
	for i, b := range stream {
		got := p.Feed([]byte{b})
		if i < len(stream)-1 && len(got) != 0 {
			t.Fatalf("frame emitted early at byte %d", i)
		}
		frames = append(frames, got...)
	}

	if len(frames) != 1 {
		t.Fatalf("got %d frames, want 1", len(frames))
	}
	want := Frame{Group: GroupRead, Reg: RegVoltageSet, Data: mustHex(t, "CD CC 44 41")}
	if !frameEqual(frames[0], want) {
		t.Errorf("frame = %+v, want %+v", frames[0], want)
	}
}

func TestParserBadChecksumResync(t *testing.T) {
	// The D9 frame is the capacity example from protocol doc section 12 as
	// printed there — its checksum byte 81 is a transcription error (the
	// rule gives 82), which makes it a genuine corrupted-frame sample.
	// The valid DA (energy) frame that follows must still be extracted.
	var p Parser
	stream := append(
		mustHex(t, "F0 A1 D9 04 9B D6 34 00 81"),    // bad CHK: must be dropped
		mustHex(t, "F0 A1 DA 04 CF AE 28 35 B8")..., // valid
	)

	frames := p.Feed(stream)

	if len(frames) != 1 {
		t.Fatalf("got %d frames, want 1", len(frames))
	}
	want := Frame{Group: GroupRead, Reg: RegEnergy, Data: mustHex(t, "CF AE 28 35")}
	if !frameEqual(frames[0], want) {
		t.Errorf("frame = %+v, want %+v", frames[0], want)
	}
}

func TestParserMultipleFramesInOneChunk(t *testing.T) {
	var p Parser
	stream := mustHex(t, "F0 A1 C0 04 00 00 A0 41 A5")                   // input voltage 20.0 V
	stream = append(stream, mustHex(t, "F0 A1 C4 04 00 00 F0 41 F9")...) // temperature 30.0 °C
	stream = append(stream, mustHex(t, "F0 A1 DD 01 01 DF")...)          // CV mode
	stream = append(stream, 0xF0, 0xA1)                                  // partial next frame

	frames := p.Feed(stream)

	if len(frames) != 3 {
		t.Fatalf("got %d frames, want 3", len(frames))
	}
	wantRegs := []Register{RegInputVoltage, RegTemperature, RegMode}
	for i, want := range wantRegs {
		if frames[i].Reg != want {
			t.Errorf("frames[%d].Reg = %#02X, want %#02X", i, byte(frames[i].Reg), byte(want))
		}
	}

	// Complete the partial frame buffered above: F0 A1 DC 01 02 DF (OCP tripped).
	frames = p.Feed(mustHex(t, "DC 01 02 DF"))
	if len(frames) != 1 {
		t.Fatalf("got %d frames after completion, want 1", len(frames))
	}
	want := Frame{Group: GroupRead, Reg: RegProtectionState, Data: []byte{0x02}}
	if !frameEqual(frames[0], want) {
		t.Errorf("frame = %+v, want %+v", frames[0], want)
	}
}

func TestParserPureGarbage(t *testing.T) {
	var p Parser
	if frames := p.Feed(mustHex(t, "01 02 03 A1 DB 01 01 DD")); frames != nil {
		t.Fatalf("got %d frames from garbage, want none", len(frames))
	}
	// A valid frame after garbage-only chunks must still parse.
	frames := p.Feed(mustHex(t, "F0 A1 DB 01 00 DC"))
	if len(frames) != 1 {
		t.Fatalf("got %d frames, want 1", len(frames))
	}
}

func TestParserSplitAtEveryPosition(t *testing.T) {
	frame := mustHex(t, "F0 A1 C3 0C CD CC 44 41 FD FF FF 3E CD CC C4 40 C3") // measurement
	for split := 1; split < len(frame); split++ {
		var p Parser
		frames := p.Feed(frame[:split])
		frames = append(frames, p.Feed(frame[split:])...)
		if len(frames) != 1 {
			t.Fatalf("split at %d: got %d frames, want 1", split, len(frames))
		}
		if frames[0].Reg != RegMeasurement || len(frames[0].Data) != 12 {
			t.Fatalf("split at %d: frame = %+v", split, frames[0])
		}
	}
}

func TestParserFalseSyncByteInsidePayload(t *testing.T) {
	// A payload containing F0 must not confuse the parser: float32 20.03 V
	// is 85 EB A0 41 — no F0, so craft one explicitly: F0 00 A0 41.
	var p Parser
	data := mustHex(t, "F0 00 A0 41")
	stream := EncodeRX(GroupRead, RegInputVoltage, data)

	frames := p.Feed(stream)

	if len(frames) != 1 {
		t.Fatalf("got %d frames, want 1", len(frames))
	}
	if !bytes.Equal(frames[0].Data, data) {
		t.Errorf("data = % X, want % X", frames[0].Data, data)
	}
}

// TestRoundtrip encodes RX frames, feeds them to the parser in small chunks
// and checks that both the frames and the decoded events survive intact.
func TestRoundtrip(t *testing.T) {
	measurement := make([]byte, 12)
	putF32(measurement, 0, 12.3)
	putF32(measurement, 4, current05())
	putF32(measurement, 8, 6.15)

	src := []Frame{
		{Group: GroupRead, Reg: RegMeasurement, Data: measurement},
		{Group: GroupRead, Reg: RegOutputEnable, Data: []byte{0x01}},
		{Group: GroupRead, Reg: RegModelName, Data: []byte("DPS-150\x00")},
		{Group: GroupRead, Reg: RegProtectionState, Data: []byte{byte(ProtectionOTP)}},
		{Group: GroupRead, Reg: RegCapacity, Data: mustHex(t, "9B D6 34 00")},
	}
	wantEvents := []Event{
		Measurement{Voltage: 12.3, Current: current05(), Power: 6.15},
		OutputState{On: true},
		DeviceInfo{Field: InfoModelName, Value: "DPS-150"},
		ProtectionState{State: ProtectionOTP},
		Capacity{AmpHours: f32(mustHex(t, "9B D6 34 00"))},
	}

	var stream []byte
	for _, f := range src {
		stream = append(stream, EncodeRX(f.Group, f.Reg, f.Data)...)
	}

	var p Parser
	var frames []Frame
	const chunkSize = 3
	for i := 0; i < len(stream); i += chunkSize {
		frames = append(frames, p.Feed(stream[i:min(i+chunkSize, len(stream))])...)
	}

	if len(frames) != len(src) {
		t.Fatalf("got %d frames, want %d", len(frames), len(src))
	}
	for i, f := range frames {
		if !frameEqual(f, src[i]) {
			t.Errorf("frames[%d] = %+v, want %+v", i, f, src[i])
		}
		event, err := Decode(f)
		if err != nil {
			t.Errorf("Decode(frames[%d]): %v", i, err)
			continue
		}
		if !reflect.DeepEqual(event, wantEvents[i]) {
			t.Errorf("events[%d] = %#v, want %#v", i, event, wantEvents[i])
		}
	}
}

func TestParserRejectsForeignGroup(t *testing.T) {
	var p Parser
	// A checksum-valid frame with GROUP 0x37: the device only ever sends
	// GROUP A1, so this must be treated as a false sync, and the valid
	// frame that follows must still be extracted.
	stream := append(mustHex(t, "F0 37 DB 01 01 DD"), mustHex(t, "F0 A1 DB 01 01 DD")...)

	frames := p.Feed(stream)

	if len(frames) != 1 {
		t.Fatalf("got %d frames, want 1", len(frames))
	}
	want := Frame{Group: GroupRead, Reg: RegOutputEnable, Data: []byte{0x01}}
	if !frameEqual(frames[0], want) {
		t.Errorf("frame = %+v, want %+v", frames[0], want)
	}
}
