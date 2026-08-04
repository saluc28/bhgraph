package client

import (
	"bytes"
	"fmt"
	"io"
	"os"
)

// DefaultSpoolThreshold is the payload size above which an ingest is spooled to
// a temporary file instead of being held in memory.
//
// Below it, a buffer is cheaper than a file and the allocation is trivial.
// Above it, the buffer is the thing that would decide how large a graph the
// caller can ingest, which is not a limit a library should impose.
const DefaultSpoolThreshold = 8 << 20 // 8 MiB

// spool accumulates bytes in memory and switches to a temporary file once it
// passes a threshold, then hands back a reader over everything written.
//
// BloodHound's server does the same on the receiving side, spilling large
// request bodies to a self-destructing temp file. Why an ingest needs it at all
// is on UploadGraph.
type spool struct {
	threshold int
	buf       bytes.Buffer
	file      *os.File
	n         int64
}

func newSpool(threshold int) *spool {
	if threshold <= 0 {
		threshold = DefaultSpoolThreshold
	}
	return &spool{threshold: threshold}
}

// Write counts only what it managed to store. size feeds Content-Length, and a
// Content-Length larger than the body leaves the server waiting for bytes that
// never arrive, so the count must not run ahead of the write.
func (s *spool) Write(p []byte) (int, error) {
	n, err := s.write(p)
	s.n += int64(n)
	return n, err
}

// write appends p, moving to a temporary file if it no longer fits under the
// threshold, and reports how many bytes of p it took.
func (s *spool) write(p []byte) (int, error) {
	if s.file != nil {
		return s.file.Write(p)
	}

	if s.buf.Len()+len(p) <= s.threshold {
		return s.buf.Write(p)
	}

	// Crossing the threshold: move what we have to a file and continue there.
	f, err := os.CreateTemp("", "bhgraph-ingest-*.json")
	if err != nil {
		return 0, fmt.Errorf("bhgraph/client: creating spool file: %w", err)
	}
	s.file = f
	if _, err := s.file.Write(s.buf.Bytes()); err != nil {
		return 0, fmt.Errorf("bhgraph/client: spilling to disk: %w", err)
	}
	s.buf.Reset()
	return s.file.Write(p)
}

// reader returns a reader over everything written, positioned at the start.
func (s *spool) reader() (io.Reader, error) {
	if s.file == nil {
		return bytes.NewReader(s.buf.Bytes()), nil
	}
	if _, err := s.file.Seek(0, io.SeekStart); err != nil {
		return nil, fmt.Errorf("bhgraph/client: rewinding spool file: %w", err)
	}
	return s.file, nil
}

// size reports how many bytes were written, for Content-Length.
func (s *spool) size() int64 { return s.n }

// spilled reports whether the spool ended up on disk. Used by tests to check
// that the threshold is honoured rather than merely configured.
func (s *spool) spilled() bool { return s.file != nil }

// close releases the temporary file, if one was created.
func (s *spool) close() error {
	if s.file == nil {
		return nil
	}
	name := s.file.Name()
	err := s.file.Close()
	if rmErr := os.Remove(name); err == nil && rmErr != nil && !os.IsNotExist(rmErr) {
		err = rmErr
	}
	s.file = nil
	return err
}
