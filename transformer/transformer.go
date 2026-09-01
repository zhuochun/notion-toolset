package transformer

import (
	"github.com/dstotijn/go-notion"
	"github.com/zhuochun/notion-toolset/notionread"
)

func New(cfg MarkdownConfig, page *notion.Page, snapshot notionread.BlockSnapshot, assetChan chan *AssetFuture) *Markdown {
	return &Markdown{
		page:       page,
		pageBlocks: snapshot.Roots(),
		snapshot:   snapshot,

		assetChan: assetChan,

		config: cfg,
	}
}
