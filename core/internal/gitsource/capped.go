package gitsource

import (
	"bytes"
	"io"
	"sync"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/format/packfile"
	"github.com/go-git/go-git/v5/storage/memory"
)

// capped is an in-memory object store that refuses a fetch larger than
// max. go-git's parser inflates each object whole before storing it, so a
// small pack holding a blob that inflates to gigabytes would be in memory
// before any count could see it. capped therefore takes the raw pack
// (PackfileWriter, at most max bytes), and before parsing it reads every
// object header: the declared size of each object and, for a delta, the
// size of what it expands to. A pack whose objects declare more than max
// in one object, or 2×max in all, is refused without inflating anything
// into memory. go-git bounds each object's inflation by its declared size.
type capped struct {
	*memory.Storage
	max int64

	mu    sync.Mutex
	limit bool
}

func (c *capped) refuse() error { c.mu.Lock(); c.limit = true; c.mu.Unlock(); return ErrTooLarge }

func (c *capped) over() bool { c.mu.Lock(); defer c.mu.Unlock(); return c.limit }

// PackfileWriter receives the pack as it arrives from the server.
func (c *capped) PackfileWriter() (io.WriteCloser, error) { return &packWriter{c: c}, nil }

type packWriter struct {
	c   *capped
	buf bytes.Buffer
}

func (w *packWriter) Write(p []byte) (int, error) {
	if int64(w.buf.Len()+len(p)) > w.c.max {
		return 0, w.c.refuse()
	}
	return w.buf.Write(p)
}

func (w *packWriter) Close() error {
	if w.c.over() {
		return ErrTooLarge
	}
	raw := w.buf.Bytes()
	if err := w.c.checkSizes(raw); err != nil {
		return err
	}
	p, err := packfile.NewParserWithStorage(packfile.NewScanner(bytes.NewReader(raw)), w.c.Storage)
	if err != nil {
		return err
	}
	_, err = p.Parse()
	w.buf = bytes.Buffer{}
	return err
}

// checkSizes walks the pack's object headers. Inflating to count goes to
// a writer that keeps nothing but a delta's first bytes.
func (c *capped) checkSizes(raw []byte) error {
	s := packfile.NewScanner(bytes.NewReader(raw))
	_, count, err := s.Header()
	if err != nil {
		return err
	}
	var total int64
	for i := uint32(0); i < count; i++ {
		h, err := s.NextObjectHeader()
		if err != nil {
			return err
		}
		size := h.Length
		if h.Type == plumbing.OFSDeltaObject || h.Type == plumbing.REFDeltaObject {
			head := &headWriter{}
			if _, _, err := s.NextObject(head); err != nil {
				return err
			}
			target, ok := deltaTarget(head.b)
			if !ok {
				return packfile.ErrMalformedPackFile
			}
			if target > c.max {
				return c.refuse()
			}
			size += target
		} else if _, _, err := s.NextObject(io.Discard); err != nil {
			return err
		}
		total += size
		if h.Length > c.max || total > 2*c.max {
			return c.refuse()
		}
	}
	return nil
}

// headWriter keeps the first 20 bytes written: enough for a delta's two
// size varints.
type headWriter struct{ b []byte }

func (h *headWriter) Write(p []byte) (int, error) {
	if n := 20 - len(h.b); n > 0 {
		if n > len(p) {
			n = len(p)
		}
		h.b = append(h.b, p[:n]...)
	}
	return len(p), nil
}

// deltaTarget reads a delta's base size and returns its target size.
func deltaTarget(b []byte) (int64, bool) {
	varint := func() (int64, bool) {
		var v int64
		for shift := uint(0); shift < 63; shift += 7 {
			if len(b) == 0 {
				return 0, false
			}
			c := b[0]
			b = b[1:]
			v |= int64(c&0x7f) << shift
			if c&0x80 == 0 {
				return v, true
			}
		}
		return 0, false
	}
	if _, ok := varint(); !ok {
		return 0, false
	}
	return varint()
}
