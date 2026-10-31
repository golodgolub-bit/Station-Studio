package main

import (
	"bytes"
	"image"
	"image/color"
	"image/gif"
	"testing"
)

func gifFixture(t *testing.T, count int) []byte {
	t.Helper()
	g := &gif.GIF{Config: image.Config{ColorModel: color.Palette{color.Black, color.White}, Width: 25, Height: 16}}
	for i := 0; i < count; i++ {
		f := image.NewPaletted(image.Rect(0, 0, 25, 16), color.Palette{color.Black, color.White})
		f.SetColorIndex(i%25, i%16, 1)
		g.Image = append(g.Image, f)
		g.Delay = append(g.Delay, 10)
	}
	var b bytes.Buffer
	if e := gif.EncodeAll(&b, g); e != nil {
		t.Fatal(e)
	}
	return b.Bytes()
}
func TestGIFPreflightAcceptsAnimation(t *testing.T) {
	if e := checkGIF(gifFixture(t, 2)); e != nil {
		t.Fatal(e)
	}
}
func TestGIFPreflightRejectsTooManyFrames(t *testing.T) {
	if checkGIF(gifFixture(t, 121)) == nil {
		t.Fatal("too many GIF frames accepted")
	}
}
func TestGIFPreflightRejectsHugeCanvas(t *testing.T) {
	data := gifFixture(t, 1)
	data[6] = 1
	data[7] = 9
	if checkGIF(data) == nil {
		t.Fatal("huge canvas accepted")
	}
}
func TestGIFPreflightRejectsTruncatedData(t *testing.T) {
	data := gifFixture(t, 2)
	if checkGIF(data[:len(data)-3]) == nil {
		t.Fatal("truncated GIF accepted")
	}
}
