package transformer

import "github.com/dstotijn/go-notion"

// BlockFuture ...
type BlockFuture struct {
	BlockID string

	done   chan struct{}
	err    error
	cached []notion.Block // access this only after blocks
}

func NewBlockFuture(blockID string) *BlockFuture {
	return &BlockFuture{
		BlockID: blockID,

		done: make(chan struct{}),
	}
}

func (f *BlockFuture) Write(b []notion.Block, err error) {
	f.cached = b
	f.err = err

	close(f.done)
}

func (f *BlockFuture) Read() ([]notion.Block, error) {
	<-f.done
	return f.cached, f.err
}
