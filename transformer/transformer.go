package transformer

import "github.com/dstotijn/go-notion"

func New(cfg MarkdownConfig, page *notion.Page, blocks []notion.Block, l chan *BlockFuture) *Markdown {
	return &Markdown{
		page:       page,
		pageBlocks: blocks,
		children:   make(map[string]*BlockFuture),

		loaderChan: l,
		config:     cfg,
	}
}
