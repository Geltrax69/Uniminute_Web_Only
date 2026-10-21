package main

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/png"
	"testing"
)

func TestImageSanitizationRejectsOversizedDimensionsAndCorruption(t *testing.T) {
	var source bytes.Buffer
	_ = png.Encode(&source, image.NewRGBA(image.Rect(0, 0, 2, 2)))
	imageBytes := source.Bytes()
	normalized, err := sanitizeImage(imageBytes, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = image.Decode(bytes.NewReader(normalized)); err != nil {
		t.Fatal("processed image is invalid")
	}
	oversized := append([]byte{}, imageBytes...)
	binary.BigEndian.PutUint32(oversized[16:20], 9000)
	binary.BigEndian.PutUint32(oversized[29:33], crc32.ChecksumIEEE(oversized[12:29]))
	if _, err = sanitizeImage(oversized, true); err == nil {
		t.Fatal("oversized dimensions accepted")
	}
	if _, err = sanitizeImage(imageBytes[:20], true); err == nil {
		t.Fatal("truncated image accepted")
	}
	imageDecodeSlots <- struct{}{}
	imageDecodeSlots <- struct{}{}
	_, err = sanitizeImage(imageBytes, true)
	<-imageDecodeSlots
	<-imageDecodeSlots
	if err == nil {
		t.Fatal("processing concurrency limit bypassed")
	}
}
