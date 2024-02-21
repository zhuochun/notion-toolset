package transformer

import (
	"bytes"
	"fmt"
	"io"
	"sync"

	"github.com/dstotijn/go-notion"
)

type Markdown struct {
	page       *notion.Page
	pageBlocks []notion.Block
	children   map[string]*BlockFuture

	queryChan chan *BlockFuture // needed to load subchildren
	assetChan chan *AssetFuture // needed to export assets

	config MarkdownConfig
}

type MarkdownConfig struct {
	NoAlias    bool   `yaml:"noAlias"`
	IndexAlias string `yaml:"indexAliasPath"` // path to files with the alias property

	NoFrontMatters bool     `yaml:"noFrontMatters"`
	FrontMatters   []string `yaml:"frontMatters"` // export fields specified only

	NoMetadata bool     `yaml:"noMetadata"`
	Metadata   []string `yaml:"metadata"` // export fields specified only as metadata

	TitleToH1    bool `yaml:"titleToH1"`
	SelectToTags bool `yaml:"selectToTags"` // apply to select properties
	PlainText    bool `yaml:"plainText"`    // make the content less clutered, no links/images/styles
}

type markdownEnv struct {
	m *Markdown
	b io.StringWriter

	parent notion.Block
	prev   notion.Block
	next   notion.Block

	index  int
	indent string

	aliasMap *sync.Map
}

func (env *markdownEnv) Copy() *markdownEnv {
	return &markdownEnv{
		m:        env.m,
		b:        env.b,
		indent:   env.indent,
		aliasMap: env.aliasMap,
	}
}

// TODO handle child pages, can export multiple files

// Transform and return the outcome in plain string, mostly for quick testing
func (m *Markdown) Transform() string {
	b := &bytes.Buffer{}
	m.TransformOut(b)
	return b.String()
}

// Transform and write to the stringWriter buffer passed in
func (m *Markdown) TransformOut(b io.StringWriter) {
	env := &markdownEnv{
		m:      m,
		b:      b,
		indent: "",
	}

	// build alias index on the fly
	// TODO cache in a temp file and read the temp file instead?
	env.aliasMap = buildAliasIndex(m.config.IndexAlias)

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

// Not atomic
func (m *Markdown) loadChildren(blockID string) {
	// check whether it is already loaded before
	if _, ok := m.children[blockID]; ok {
		return
	}

	block := NewBlockFuture(blockID)
	m.children[blockID] = block
	m.queryChan <- block
}

// Read the children
func (m *Markdown) getChildren(blockID string) ([]notion.Block, error) {
	f, ok := m.children[blockID]
	if !ok {
		return []notion.Block{}, fmt.Errorf("failed to create blockID: %v", blockID)
	}

	childBlocks, err := f.Read()
	return childBlocks, err
}
