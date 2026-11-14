package gitsource

import (
	"io"
	"sync"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/storage/memory"
)

// capped is an in-memory object store that refuses to hold more than max
// bytes of inflated objects. Every object is counted while it is written,
// so a small pack that inflates to a huge blob stops at the limit instead
// of after it.
type capped struct {
	*memory.Storage
	max int64

	mu    sync.Mutex
	used  int64
	limit bool
}

func (c *capped) take(n int64) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.used += n
	if c.used > c.max {
		c.limit = true
	}
	return !c.limit
}

func (c *capped) over() bool { c.mu.Lock(); defer c.mu.Unlock(); return c.limit }

// NewEncodedObject hands out objects whose writes are counted.
func (c *capped) NewEncodedObject() plumbing.EncodedObject {
	return &countedObject{MemoryObject: &plumbing.MemoryObject{}, c: c}
}

// SetEncodedObject also counts objects made elsewhere (the size a writer
// already counted is not counted again).
func (c *capped) SetEncodedObject(o plumbing.EncodedObject) (plumbing.Hash, error) {
	if co, ok := o.(*countedObject); ok {
		if c.over() {
			return plumbing.ZeroHash, ErrTooLarge
		}
		return c.Storage.SetEncodedObject(co.MemoryObject)
	}
	if !c.take(o.Size()) {
		return plumbing.ZeroHash, ErrTooLarge
	}
	return c.Storage.SetEncodedObject(o)
}

type countedObject struct {
	*plumbing.MemoryObject
	c *capped
}

func (o *countedObject) Writer() (io.WriteCloser, error) {
	w, err := o.MemoryObject.Writer()
	if err != nil {
		return nil, err
	}
	return &countedWriter{w: w, c: o.c}, nil
}

func (o *countedObject) Write(p []byte) (int, error) {
	if !o.c.take(int64(len(p))) {
		return 0, ErrTooLarge
	}
	return o.MemoryObject.Write(p)
}

type countedWriter struct {
	w io.WriteCloser
	c *capped
}

func (w *countedWriter) Write(p []byte) (int, error) {
	if !w.c.take(int64(len(p))) {
		return 0, ErrTooLarge
	}
	return w.w.Write(p)
}

func (w *countedWriter) Close() error { return w.w.Close() }
