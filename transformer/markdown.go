package transformer

import (
	"bytes"
	"fmt"
	"io"

	"github.com/dstotijn/go-notion"
)

type Markdown struct {
	page       *notion.Page
	pageBlocks []notion.Block
	children   map[string]*BlockFuture

	queryChan chan *BlockFuture
	assetChan chan *AssetFuture

	config MarkdownConfig
}

type MarkdownConfig struct {
	NoAlias        bool     `yaml:"noAlias"`
	NoFrontMatters bool     `yaml:"noFrontMatters"`
	TitleToH1      bool     `yaml:"titleToH1"`
	SelectToTags   bool     `yaml:"selectToTags"`
	FrontMatters   []string `yaml:"frontMatters"`
	Metadata       []string `yaml:"metadata"`
}

type markdownEnv struct {
	m *Markdown
	b io.StringWriter

	prev   *notion.Block
	parent *notion.Block

	index  int
	indent string
}

func (env *markdownEnv) Copy() *markdownEnv {
	return &markdownEnv{
		m:      env.m,
		b:      env.b,
		prev:   env.prev,
		parent: env.parent,
		indent: env.indent,
	}
}

// TODO handle child pages, can export multiple files

func (m *Markdown) Transform() string {
	b := &bytes.Buffer{}
	m.TransformOut(b)
	return b.String()
}

func (m *Markdown) TransformOut(b io.StringWriter) {
	env := &markdownEnv{
		m:      m,
		b:      b,
		indent: "",
	}

	// write page properties as front matters
	if m.page != nil {
		m.transformFrontMatter(env, m.page)
	}

	// special case
	if m.config.TitleToH1 {
		env.b.WriteString("# ")
		title, _ := GetPageTitle(*m.page)
		env.b.WriteString(title)
		env.b.WriteString("\n\n")
	}

	// write page properties as metadata
	if m.page != nil {
		m.transformMetadata(env, m.page)
	}

	// write page blocks
	m.transformBlocks(env, m.pageBlocks)
}

func (m *Markdown) isListType(t notion.BlockType) bool {
	return t == notion.BlockTypeBulletedListItem || t == notion.BlockTypeNumberedListItem
}

func (m *Markdown) getChildren(blockID string) ([]notion.Block, error) {
	f, ok := m.children[blockID]
	if !ok {
		return []notion.Block{}, fmt.Errorf("failed to create blockID: %v", blockID)
	}

	childBlocks, err := f.Read()
	return childBlocks, err
}
