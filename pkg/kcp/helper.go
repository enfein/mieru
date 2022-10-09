package kcp

import "encoding/binary"

// Encode 8 bits unsigned int.
func encode8u(p []byte, c byte) []byte {
	p[0] = c
	return p[1:]
}

// Decode 8 bits unsigned int.
func decode8u(p []byte, c *byte) []byte {
	*c = p[0]
	return p[1:]
}

// Encode 16 bits unsigned int (LSB).
func encode16u(p []byte, w uint16) []byte {
	binary.LittleEndian.PutUint16(p, w)
	return p[2:]
}

// Decode 16 bits unsigned int (LSB).
func decode16u(p []byte, w *uint16) []byte {
	*w = binary.LittleEndian.Uint16(p)
	return p[2:]
}

// Encode 32 bits unsigned int (LSB).
func encode32u(p []byte, l uint32) []byte {
	binary.LittleEndian.PutUint32(p, l)
	return p[4:]
}

// Decode 32 bits unsigned int (LSB).
func decode32u(p []byte, l *uint32) []byte {
	*l = binary.LittleEndian.Uint32(p)
	return p[4:]
}

// Returns the difference between two uint32 as int32.
func timediff(later, earlier uint32) int32 {
	return (int32)(later - earlier)
}
