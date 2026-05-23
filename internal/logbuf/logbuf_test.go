package logbuf

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRingBufferWrite(t *testing.T) {
	rb := NewRingBuffer(3)
	rb.Write([]byte("line1\n"))
	rb.Write([]byte("line2\n"))
	rb.Write([]byte("line3\n"))

	lines := rb.Lines()
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(lines))
	}
	if lines[0] != "line1" || lines[1] != "line2" || lines[2] != "line3" {
		t.Fatalf("unexpected lines: %v", lines)
	}
}

func TestRingBufferOverflow(t *testing.T) {
	rb := NewRingBuffer(2)
	rb.Write([]byte("line1\n"))
	rb.Write([]byte("line2\n"))
	rb.Write([]byte("line3\n"))

	lines := rb.Lines()
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines after overflow, got %d", len(lines))
	}
	if lines[0] != "line2" || lines[1] != "line3" {
		t.Fatalf("expected oldest dropped, got: %v", lines)
	}
}

func TestRingBufferNoTrailingNewline(t *testing.T) {
	rb := NewRingBuffer(10)
	rb.Write([]byte("partial"))

	lines := rb.Lines()
	if len(lines) != 1 {
		t.Fatalf("expected 1 line, got %d", len(lines))
	}
	if lines[0] != "partial" {
		t.Fatalf("expected 'partial', got %q", lines[0])
	}
}

func TestRingBufferEmpty(t *testing.T) {
	rb := NewRingBuffer(10)
	lines := rb.Lines()
	if len(lines) != 0 {
		t.Fatalf("expected 0 lines, got %d", len(lines))
	}
}

func TestMultiWriterWritesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")

	mw := NewMultiWriter(100, path)
	mw.Write([]byte("hello\n"))
	mw.Close()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "hello") {
		t.Fatalf("file should contain 'hello', got: %q", string(data))
	}
}

func TestMultiWriterRingBuffer(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")

	mw := NewMultiWriter(100, path)
	mw.Write([]byte("hello\n"))
	mw.Close()

	lines := mw.Lines()
	if len(lines) != 1 || lines[0] != "hello" {
		t.Fatalf("ring buffer should have 'hello', got: %v", lines)
	}
}
