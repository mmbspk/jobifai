package main

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
)

var formVisionFixtures = []struct {
	file   string
	fields []map[string]any
}{
	{"text_input.png", []map[string]any{{"type": "text", "question": "Full name", "options": []string{}}}},
	{"textarea.png", []map[string]any{{"type": "text", "question": "Cover note", "options": []string{}}}},
	{"select.png", []map[string]any{{"type": "select", "question": "Country", "options": []string{"Australia", "New Zealand"}}}},
	{"radio_group.png", []map[string]any{{"type": "radio", "question": "Work authorization", "options": []string{"Yes", "No"}}}},
	{"multi_field.png", []map[string]any{
		{"type": "text", "question": "Email", "options": []string{}},
		{"type": "text", "question": "Phone", "options": []string{}},
	}},
	{"required_marker.png", []map[string]any{{"type": "text", "question": "Full name (required)", "options": []string{}}}},
	{"long_label.png", []map[string]any{{"type": "text", "question": "Describe your experience coordinating multi-site logistics programs", "options": []string{}}}},
	{"disabled_field.png", []map[string]any{{"type": "text", "question": "Employee ID (disabled)", "options": []string{}}}},
	{"filled_ignore.png", []map[string]any{{"type": "text", "question": "Reference number", "options": []string{}}}},
	{"validation_error.png", []map[string]any{{"type": "text", "question": "Postal code", "options": []string{}}}},
	{"two_column.png", []map[string]any{
		{"type": "text", "question": "City", "options": []string{}},
		{"type": "text", "question": "State", "options": []string{}},
	}},
	{"similar_options.png", []map[string]any{{"type": "radio", "question": "Preferred contact", "options": []string{"Email", "E-mail"}}}},
}

func ensureFormVisionFixtures(root string) error {
	dir := filepath.Join(root, "form_vision", "fixtures")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for _, fx := range formVisionFixtures {
		if err := writeFormPNG(filepath.Join(dir, fx.file)); err != nil {
			return err
		}
	}
	return nil
}

func copyFormVisionFixtures(srcRoot, destDir string) error {
	src := filepath.Join(srcRoot, "form_vision", "fixtures")
	dst := filepath.Join(destDir, "fixtures")
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		b, err := os.ReadFile(filepath.Join(src, e.Name()))
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dst, e.Name()), b, 0o644); err != nil {
			return err
		}
	}
	return nil
}

func writeFormPNG(path string) error {
	w, h := 640, 480
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.White)
		}
	}
	// simple form chrome
	for x := 40; x < 600; x++ {
		img.Set(x, 120, color.Black)
		img.Set(x, 220, color.Black)
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	return png.Encode(f, img)
}
