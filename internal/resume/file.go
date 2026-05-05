// Package resume also provides file-to-text extraction and PDF generation.
package resume

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/ledongthuc/pdf"
)

// TextFromReader reads bytes from r and returns them as plain text suitable
// for sending to an LLM. Handles .txt/.md/.yaml natively, extracts text from
// .docx by unzipping word/document.xml, and extracts text from .pdf using
// ledongthuc/pdf.
func TextFromReader(r io.Reader, filename string) (string, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return "", fmt.Errorf("read file: %w", err)
	}

	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".txt", ".md", ".yaml", ".yml":
		return string(data), nil

	case ".docx", ".doc":
		text, err := extractDocxText(data)
		if err != nil {
			return "", fmt.Errorf("could not extract text from DOCX: %w, try saving as .txt", err)
		}
		if len(strings.TrimSpace(text)) < 50 {
			return "", fmt.Errorf("DOCX appears empty or has no readable text, try saving as .txt")
		}
		return text, nil

	case ".pdf":
		text, err := extractPDFText(data)
		if err != nil {
			return "", fmt.Errorf("could not extract text from PDF: %w, try uploading a .docx or .txt", err)
		}
		if len(strings.TrimSpace(text)) < 50 {
			return "", fmt.Errorf("PDF appears to be image-only (scanned), please upload a .docx or .txt version")
		}
		return text, nil

	default:
		return string(data), nil
	}
}

// extractPDFText extracts plain text from a PDF using ledongthuc/pdf.
func extractPDFText(data []byte) (string, error) {
	ra := bytes.NewReader(data)
	pr, err := pdf.NewReader(ra, int64(len(data)))
	if err != nil {
		return "", err
	}
	var buf strings.Builder
	for i := 1; i <= pr.NumPage(); i++ {
		page := pr.Page(i)
		if page.V.IsNull() {
			continue
		}
		text, err := page.GetPlainText(nil)
		if err != nil {
			continue
		}
		buf.WriteString(text)
		buf.WriteRune('\n')
	}
	return buf.String(), nil
}

// extractDocxText unzips a DOCX (Office Open XML) and returns the plain text
// from word/document.xml with XML tags stripped.
func extractDocxText(data []byte) (string, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", fmt.Errorf("not a valid DOCX (zip) file: %w", err)
	}
	for _, f := range zr.File {
		if f.Name != "word/document.xml" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return "", err
		}
		defer rc.Close()
		xmlData, err := io.ReadAll(rc)
		if err != nil {
			return "", err
		}
		return xmlToPlainText(string(xmlData)), nil
	}
	return "", fmt.Errorf("word/document.xml not found inside DOCX")
}

// xmlToPlainText strips XML tags and inserts newlines at paragraph/row
// boundaries so the resulting text is readable by an LLM.
func xmlToPlainText(s string) string {
	s = strings.ReplaceAll(s, "</w:p>", "\n")
	s = strings.ReplaceAll(s, "</w:tr>", "\n")

	var buf strings.Builder
	inTag := false
	for _, r := range s {
		switch r {
		case '<':
			inTag = true
		case '>':
			inTag = false
		default:
			if !inTag {
				buf.WriteRune(r)
			}
		}
	}

	var lines []string
	for _, line := range strings.Split(buf.String(), "\n") {
		if t := strings.TrimSpace(line); t != "" {
			lines = append(lines, t)
		}
	}
	return strings.Join(lines, "\n")
}
