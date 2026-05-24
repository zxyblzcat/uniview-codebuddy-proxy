package logbuf

import (
	"bytes"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
)

// RingBuffer is a fixed-size in-memory ring buffer that stores the most recent N log lines.
type RingBuffer struct {
	mu      sync.Mutex
	buf     []string
	size    int
	head    int
	count   int
	partial []byte
}

// NewRingBuffer creates a ring buffer that retains at most size lines.
func NewRingBuffer(size int) *RingBuffer {
	return &RingBuffer{
		buf:  make([]string, size),
		size: size,
	}
}

// Write appends data to the ring buffer, splitting on newlines.
func (rb *RingBuffer) Write(p []byte) (int, error) {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	n := len(p)
	off := 0
	for {
		idx := bytes.IndexByte(p[off:], '\n')
		if idx < 0 {
			rb.partial = append(rb.partial, p[off:]...)
			break
		}
		rb.partial = append(rb.partial, p[off:off+idx]...)
		rb.push(string(rb.partial))
		rb.partial = rb.partial[:0]
		off = off + idx + 1
	}
	return n, nil
}

func (rb *RingBuffer) push(line string) {
	rb.buf[rb.head] = line
	rb.head = (rb.head + 1) % rb.size
	if rb.count < rb.size {
		rb.count++
	}
}

// Lines returns all stored lines in order (oldest first).
// Any partial line (not yet terminated by newline) is included as the last entry.
func (rb *RingBuffer) Lines() []string {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	if rb.count == 0 && len(rb.partial) == 0 {
		return nil
	}

	result := make([]string, 0, rb.count+1)
	start := (rb.head - rb.count + rb.size) % rb.size
	for i := 0; i < rb.count; i++ {
		idx := (start + i) % rb.size
		result = append(result, rb.buf[idx])
	}
	if len(rb.partial) > 0 {
		result = append(result, string(rb.partial))
	}
	return result
}

// subscriber receives new log lines as they are written.
type subscriber struct {
	ch chan string
}

// MultiWriter writes to both a RingBuffer and a log file.
type MultiWriter struct {
	ring        *RingBuffer
	file        *os.File
	mu          sync.Mutex
	subscribers map[*subscriber]struct{}
}

// NewMultiWriter creates a MultiWriter that writes to an in-memory ring buffer
// of ringSize lines and appends to the file at filePath.
func NewMultiWriter(ringSize int, filePath string) *MultiWriter {
	mw := &MultiWriter{
		ring:        NewRingBuffer(ringSize),
		subscribers: make(map[*subscriber]struct{}),
	}

	if filePath != "" {
		dir := filepath.Dir(filePath)
		if err := os.MkdirAll(dir, 0700); err == nil {
			f, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
			if err == nil {
				mw.file = f
			}
		}
	}

	return mw
}

// Write implements io.Writer — writes to both ring buffer and file.
func (mw *MultiWriter) Write(p []byte) (int, error) {
	mw.mu.Lock()
	defer mw.mu.Unlock()

	n, _ := mw.ring.Write(p)
	if mw.file != nil {
		if _, err := mw.file.Write(p); err != nil {
			log.Printf("Warning: log file write error: %v", err)
		}
	}

	// Notify subscribers with new complete lines.
	mw.notifySubscribers(p)

	return n, nil
}

// notifySubscribers sends new complete log lines to all subscribers.
// Must be called with mw.mu held.
func (mw *MultiWriter) notifySubscribers(p []byte) {
	off := 0
	for {
		idx := bytes.IndexByte(p[off:], '\n')
		if idx < 0 {
			break
		}
		line := string(p[off : off+idx])
		off = off + idx + 1
		for sub := range mw.subscribers {
			select {
			case sub.ch <- line:
			default:
			}
		}
	}
}

// Subscribe returns a channel that receives new log lines as they are written,
// and an unsubscribe function. The channel is buffered (256); slow consumers
// have lines dropped.
func (mw *MultiWriter) Subscribe() (<-chan string, func()) {
	mw.mu.Lock()
	defer mw.mu.Unlock()

	ch := make(chan string, 256)
	sub := &subscriber{ch: ch}
	mw.subscribers[sub] = struct{}{}

	unsubscribe := func() {
		mw.mu.Lock()
		defer mw.mu.Unlock()
		delete(mw.subscribers, sub)
	}

	return ch, unsubscribe
}

// Lines returns the ring buffer contents.
func (mw *MultiWriter) Lines() []string {
	return mw.ring.Lines()
}

// Clear truncates both the in-memory ring buffer and the log file.
func (mw *MultiWriter) Clear() {
	mw.mu.Lock()
	defer mw.mu.Unlock()

	// Reset ring buffer
	mw.ring.mu.Lock()
	mw.ring.head = 0
	mw.ring.count = 0
	mw.ring.partial = mw.ring.partial[:0]
	mw.ring.mu.Unlock()

	// Truncate log file
	if mw.file != nil {
		mw.file.Truncate(0)
		mw.file.Seek(0, io.SeekStart)
	}
}

// Close closes the log file if open.
func (mw *MultiWriter) Close() {
	mw.mu.Lock()
	defer mw.mu.Unlock()
	if mw.file != nil {
		mw.file.Close()
		mw.file = nil
	}
}

// Ensure interfaces are satisfied.
var (
	_ io.Writer = (*RingBuffer)(nil)
	_ io.Writer = (*MultiWriter)(nil)
)
