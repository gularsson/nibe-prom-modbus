package main

import "testing"

// Nibe S-series sends 32-bit values as two 16-bit registers, LOW word first.
// The cases below are real readings from an S1255 on 2026-11-08, when the
// exporter still decoded big-endian and reported every u32 value x65536.
func TestDecode(t *testing.T) {
	cases := []struct {
		name string
		typ  string
		raw  []byte
		want float64
	}{
		// reg 2166 (watts, factor 1): 882 W. Previously decoded as 57802752.
		{"u32 low word first", "u32", []byte{0x03, 0x72, 0x00, 0x00}, 882},
		// A value above 65535 must combine both words: 0x0001_0002 = 65538.
		{"u32 both words", "u32", []byte{0x00, 0x02, 0x00, 0x01}, 65538},
		// reg 11 degree minutes, e.g. -123.4 DM sent as -1234.
		{"s32 negative", "s32", []byte{0xFB, 0x2E, 0xFF, 0xFF}, -1234},
		{"s32 positive", "s32", []byte{0x00, 0x64, 0x00, 0x00}, 100},
		{"u16", "u16", []byte{0x01, 0x2C}, 300},
		{"s16 negative", "s16", []byte{0xFF, 0x9C}, -100},
		// u8 values occupy a full 16-bit register.
		{"u8", "u8", []byte{0x00, 0x03}, 3},
		// s8: low byte, signed, whether or not the high byte is sign-extended.
		{"s8 positive", "s8", []byte{0x00, 0x04}, 4},
		{"s8 negative sign-extended", "s8", []byte{0xFF, 0xFE}, -2},
		{"s8 negative not extended", "s8", []byte{0x00, 0xFE}, -2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := decode(tc.typ, tc.raw)
			if err != nil {
				t.Fatalf("decode(%q): %v", tc.typ, err)
			}
			if got != tc.want {
				t.Errorf("decode(%q, % x) = %v, want %v", tc.typ, tc.raw, got, tc.want)
			}
		})
	}
}

func TestDecodeRejectsShortInput(t *testing.T) {
	if _, err := decode("u32", []byte{0x00, 0x01}); err == nil {
		t.Error("expected an error for a u32 with only one register")
	}
}

func TestRegisterCount(t *testing.T) {
	for typ, want := range map[string]uint16{"u8": 1, "s8": 1, "u16": 1, "s16": 1, "u32": 2, "s32": 2} {
		got, err := registerCount(typ)
		if err != nil || got != want {
			t.Errorf("registerCount(%q) = %d, %v; want %d", typ, got, err, want)
		}
	}
	if _, err := registerCount("f64"); err == nil {
		t.Error("expected an error for an unsupported type")
	}
}
