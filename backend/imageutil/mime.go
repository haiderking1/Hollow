package imageutil

import (
	"bytes"
	"encoding/binary"
)

var pngSignature = []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}

func DetectSupportedImageMimeType(data []byte) string {
	if len(data) >= 3 && bytes.Equal(data[:3], []byte{0xff, 0xd8, 0xff}) {
		if len(data) > 3 && data[3] == 0xf7 {
			return ""
		}
		return "image/jpeg"
	}
	if len(data) >= len(pngSignature) && bytes.Equal(data[:len(pngSignature)], pngSignature) {
		if isPng(data) && !isAnimatedPng(data) {
			return "image/png"
		}
		return ""
	}
	if len(data) >= 3 && string(data[:3]) == "GIF" {
		return "image/gif"
	}
	if len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP" {
		return "image/webp"
	}
	return ""
}

func isPng(data []byte) bool {
	if len(data) < 16 {
		return false
	}
	length := binary.BigEndian.Uint32(data[len(pngSignature) : len(pngSignature)+4])
	return length == 13 && string(data[12:16]) == "IHDR"
}

func isAnimatedPng(data []byte) bool {
	offset := len(pngSignature)
	for offset+8 <= len(data) {
		chunkLength := binary.BigEndian.Uint32(data[offset : offset+4])
		chunkType := string(data[offset+4 : offset+8])
		if chunkType == "acTL" {
			return true
		}
		if chunkType == "IDAT" {
			return false
		}
		nextOffset := offset + 8 + int(chunkLength) + 4
		if nextOffset <= offset || nextOffset > len(data) {
			return false
		}
		offset = nextOffset
	}
	return false
}
