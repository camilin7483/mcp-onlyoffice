package main

import (
	"os"

	"github.com/cam/mcp-onlyoffice/internal/ooxml"
)

func main() {
	data, err := ooxml.CreatePPTXFromSlides([]ooxml.SlideData{
		{Title: "Cover & <Intro>", Content: "Line 1\nLine 2"},
		{Title: "Second"},
	})
	if err != nil {
		panic(err)
	}
	if err := os.WriteFile("/tmp/opencode/go_native.pptx", data, 0644); err != nil {
		panic(err)
	}
}
