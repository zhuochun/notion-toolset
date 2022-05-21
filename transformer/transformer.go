package transformer

import "github.com/dstotijn/go-notion"

func New(cfg MarkdownConfig, page *notion.Page, blocks []notion.Block, queryChan chan *BlockFuture, assetChan chan *AssetFuture) *Markdown {
	return &Markdown{
		page:       page,
		pageBlocks: blocks,
		children:   make(map[string]*BlockFuture),

		queryChan: queryChan,
		assetChan: assetChan,

		config: cfg,
	}
}
