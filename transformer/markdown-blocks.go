package transformer

import (
	"log"
	"strings"

	"github.com/dstotijn/go-notion"
)

func (m *Markdown) transformBlocks(env *markdownEnv, blocks []notion.Block) {
	for i, block := range blocks {
		env.index = i

		if i-1 >= 0 {
			env.prev = blocks[i-1]
		}
		if i+1 < len(blocks) {
			env.next = blocks[i+1]
		}

		m.transformBlock(env, block)
	}
}

// https://developers.notion.com/reference/block
func (m *Markdown) transformBlock(env *markdownEnv, block notion.Block) bool {
	switch b := block.(type) {
	case *notion.ParagraphBlock:
		m.markdownParagraph(env, b)
	case *notion.Heading1Block:
		m.markdownHeading1(env, b)
	case *notion.Heading2Block:
		m.markdownHeading2(env, b)
	case *notion.Heading3Block:
		m.markdownHeading3(env, b)
	case *notion.BulletedListItemBlock:
		m.markdownBulletedListItem(env, b)
	case *notion.NumberedListItemBlock:
		m.markdownNumberedListItem(env, b)
	case *notion.ToDoBlock:
		m.markdownToDo(env, b)
	case *notion.ToggleBlock:
		m.markdownToggle(env, b)
	case *notion.ChildPageBlock:
		return false // TODO
	case *notion.ChildDatabaseBlock:
		return false // TODO
	case *notion.CalloutBlock:
		m.markdownCallout(env, b)
	case *notion.QuoteBlock:
		m.markdownQuote(env, b)
	case *notion.CodeBlock:
		m.markdownCode(env, b)
	case *notion.EmbedBlock:
		return false // TODO
	case *notion.ImageBlock:
		m.markdownImage(env, b)
	case *notion.AudioBlock:
		return false // TODO
	case *notion.VideoBlock:
		m.markdownVideo(env, b)
	case *notion.FileBlock:
		return false // TODO
	case *notion.PDFBlock:
		return false // TODO
	case *notion.BookmarkBlock:
		m.markdownBookmark(env, b)
	case *notion.EquationBlock:
		m.markdownEquation(env, b)
	case *notion.DividerBlock:
		m.markdownDivider(env, b)
	case *notion.TableOfContentsBlock:
		return false // Skip
	case *notion.BreadcrumbBlock:
		return false // Skip
	case *notion.ColumnListBlock:
		m.markdownColumnList(env, b)
	case *notion.ColumnBlock:
		m.markdownColumn(env, b)
	case *notion.TableBlock:
		m.markdownTable(env, b)
	case *notion.TableRowBlock:
		m.markdownTableRow(env, b)
	case *notion.LinkPreviewBlock:
		m.markdownLinkPreview(env, b)
	case *notion.LinkToPageBlock:
		m.markdownLinkToPage(env, b)
	case *notion.SyncedBlock:
		m.markdownSyncedBlock(env, b)
	case *notion.TemplateBlock:
		return false // TODO
	case *notion.UnsupportedBlock:
		return false // TODO
	default:
		return false // TODO
	}

	return true
}

func (m *Markdown) markdownRichText(env *markdownEnv, text notion.RichText) {
	m.markdownAnnotation(env, text, true)
	env.b.WriteString(text.PlainText)
	m.markdownAnnotation(env, text, false)
}

func (m *Markdown) markdownAnnotation(env *markdownEnv, text notion.RichText, prefix bool) {
	if env.m.config.PlainText {
		return
	}

	switch {
	case text.Annotations.Bold:
		env.b.WriteString("**")
	case text.Annotations.Italic:
		env.b.WriteString("_")
	case text.Annotations.Strikethrough:
		env.b.WriteString("~")
	case text.Annotations.Code:
		env.b.WriteString("`")
	}

	if text.Type == notion.RichTextTypeMention && text.Mention.Type == notion.MentionTypePage {
		// mention write as internal reference [[link|title]]
		if prefix {
			env.b.WriteString("[[")
			env.b.WriteString(SimpleAliasOrID(text.Mention.Page.ID, env.aliasMap))
			env.b.WriteString("|")
		} else {
			env.b.WriteString("]]")
		}

	} else if text.HRef != nil && *text.HRef != "" {
		// write as normal external link
		if prefix {
			env.b.WriteString("[")
		} else {
			env.b.WriteString("](")
			env.b.WriteString(*text.HRef)
			env.b.WriteString(")")
		}
	}
}

// refers to children that do not need to special case in markdown (not directly convertable)
func (m *Markdown) markdownPlainChildren(env *markdownEnv, block notion.Block) {
	if !block.HasChildren() {
		return
	}

	m.loadChildren(block.ID())

	blocks, err := m.getChildren(block.ID())
	if err != nil {
		log.Printf("Error fetch children of id: %v, page: %+v, err: %v", block.ID(), block.Parent(), err)
	}

	newEnv := env.Copy()
	newEnv.prev = block
	newEnv.parent = nil

	m.transformBlocks(newEnv, blocks)
}

func (m *Markdown) markdownParagraph(env *markdownEnv, block *notion.ParagraphBlock) {
	env.b.WriteString(env.indent)

	for _, text := range block.RichText {
		m.markdownRichText(env, text)
	}
	env.b.WriteString("\n\n")

	m.markdownPlainChildren(env, block)
}

func (m *Markdown) markdownHeading1(env *markdownEnv, block *notion.Heading1Block) {
	env.b.WriteString(env.indent)

	env.b.WriteString("## ")
	for _, text := range block.RichText {
		env.b.WriteString(text.PlainText)
	}
	env.b.WriteString("\n\n")

	m.markdownPlainChildren(env, block)
}

func (m *Markdown) markdownHeading2(env *markdownEnv, block *notion.Heading2Block) {
	env.b.WriteString(env.indent)

	env.b.WriteString("### ")
	for _, text := range block.RichText {
		env.b.WriteString(text.PlainText)
	}
	env.b.WriteString("\n\n")

	m.markdownPlainChildren(env, block)
}

func (m *Markdown) markdownHeading3(env *markdownEnv, block *notion.Heading3Block) {
	env.b.WriteString(env.indent)

	env.b.WriteString("#### ")
	for _, text := range block.RichText {
		env.b.WriteString(text.PlainText)
	}
	env.b.WriteString("\n\n")

	m.markdownPlainChildren(env, block)
}

// Sublist items in children
func (m *Markdown) markdownSublistChildren(env *markdownEnv, block notion.Block) {
	if !block.HasChildren() {
		return
	}

	m.loadChildren(block.ID())

	blocks, err := m.getChildren(block.ID())
	if err != nil {
		log.Printf("Error fetch children of id: %v, page: %+v, err: %v", block.ID(), block.Parent(), err)
	}

	newEnv := env.Copy()
	newEnv.prev = block
	newEnv.parent = block
	newEnv.indent += "  "

	m.transformBlocks(newEnv, blocks)
}

func (m *Markdown) markdownBulletedListItem(env *markdownEnv, block *notion.BulletedListItemBlock) {
	env.b.WriteString(env.indent)

	env.b.WriteString("- ")
	for _, text := range block.RichText {
		m.markdownRichText(env, text)
	}
	env.b.WriteString("\n\n")

	m.markdownSublistChildren(env, block)
}

func (m *Markdown) markdownNumberedListItem(env *markdownEnv, block *notion.NumberedListItemBlock) {
	env.b.WriteString(env.indent)

	env.b.WriteString("0. ") // env.index+1 doesn't work, need a local index
	for _, text := range block.RichText {
		m.markdownRichText(env, text)
	}
	env.b.WriteString("\n\n")

	m.markdownSublistChildren(env, block)
}

func (m *Markdown) markdownToDo(env *markdownEnv, block *notion.ToDoBlock) {
	env.b.WriteString(env.indent)

	if *block.Checked {
		env.b.WriteString("- [x] ")
	} else {
		env.b.WriteString("- [ ] ")
	}

	for _, text := range block.RichText {
		m.markdownRichText(env, text)
	}
	env.b.WriteString("\n\n")

	m.markdownSublistChildren(env, block)
}

func (m *Markdown) markdownToggle(env *markdownEnv, block *notion.ToggleBlock) {
	env.b.WriteString(env.indent)

	for _, text := range block.RichText {
		m.markdownRichText(env, text)
	}

	env.b.WriteString("\n\n")

	m.markdownPlainChildren(env, block)
}

func (m *Markdown) markdownCallout(env *markdownEnv, block *notion.CalloutBlock) {
	env.b.WriteString(env.indent)
	env.b.WriteString("> ")

	for _, text := range block.RichText {
		m.markdownRichText(env, text)
	}

	env.b.WriteString("\n\n")

	m.markdownPlainChildren(env, block)
}

func (m *Markdown) markdownQuote(env *markdownEnv, block *notion.QuoteBlock) {
	env.b.WriteString(env.indent)
	env.b.WriteString("> ")

	for _, text := range block.RichText {
		m.markdownRichText(env, text)
	}

	env.b.WriteString("\n\n")

	m.markdownPlainChildren(env, block)
}

func (m *Markdown) markdownCode(env *markdownEnv, block *notion.CodeBlock) {
	env.b.WriteString(env.indent)
	env.b.WriteString("```")

	if block.Language != nil {
		env.b.WriteString(" ")
		env.b.WriteString(*block.Language)
	}
	env.b.WriteString("\n")

	for _, text := range block.RichText {
		env.b.WriteString(text.PlainText)
	}

	env.b.WriteString("\n```\n\n")
}

func (m *Markdown) markdownImage(env *markdownEnv, block *notion.ImageBlock) {
	if m.config.PlainText {
		return
	}

	var filename string
	var err error

	if block.Type == notion.FileTypeExternal {
		filename = block.External.URL
	} else if env.m.assetChan != nil {
		asset := NewAssetFuture(block.ID(), block.File.URL)
		env.m.assetChan <- asset

		filename, err = asset.Read()
	}

	if err != nil {
		return
	}

	env.b.WriteString(env.indent)
	env.b.WriteString("![")

	for _, text := range block.Caption {
		env.b.WriteString(text.PlainText)
	}

	env.b.WriteString("](")
	env.b.WriteString(filename)
	env.b.WriteString(")\n\n")
}

func (m *Markdown) markdownVideo(env *markdownEnv, block *notion.VideoBlock) {
	if m.config.PlainText {
		return
	}

	env.b.WriteString(env.indent)
	env.b.WriteString("[")

	for _, text := range block.Caption {
		env.b.WriteString(text.PlainText)
	}

	env.b.WriteString("](")

	if block.Type == notion.FileTypeExternal {
		env.b.WriteString(block.External.URL)
	} else {
		env.b.WriteString(block.File.URL) // TODO downlaod
	}

	env.b.WriteString(")\n\n")
}

func (m *Markdown) markdownBookmark(env *markdownEnv, block *notion.BookmarkBlock) {
	if m.config.PlainText {
		return
	}

	env.b.WriteString(env.indent)

	hasCaption := len(block.Caption) > 0

	if hasCaption {
		env.b.WriteString("[")
		for _, text := range block.Caption {
			env.b.WriteString(text.PlainText)
		}
		env.b.WriteString("](")
	}

	env.b.WriteString(block.URL)

	if hasCaption {
		env.b.WriteString(")")
	}

	env.b.WriteString("\n\n")
}

func (m *Markdown) markdownEquation(env *markdownEnv, block *notion.EquationBlock) {
	env.b.WriteString(env.indent)
	env.b.WriteString("$$\n")
	env.b.WriteString(block.Expression)
	env.b.WriteString("\n$$\n\n")
}

func (m *Markdown) markdownDivider(env *markdownEnv, block *notion.DividerBlock) {
	env.b.WriteString(env.indent)
	env.b.WriteString("---\n\n")
}

func (m *Markdown) markdownColumnList(env *markdownEnv, block *notion.ColumnListBlock) {
	m.markdownPlainChildren(env, block)
}

func (m *Markdown) markdownColumn(env *markdownEnv, block *notion.ColumnBlock) {
	m.markdownPlainChildren(env, block)
}

func (m *Markdown) markdownTable(env *markdownEnv, block *notion.TableBlock) {
	m.markdownPlainChildren(env, block)

	env.b.WriteString("\n")
}

func (m *Markdown) markdownTableRow(env *markdownEnv, block *notion.TableRowBlock) {
	for _, cells := range block.Cells {
		env.b.WriteString("| ")

		for _, cell := range cells {
			m.markdownAnnotation(env, cell, true)
			env.b.WriteString(strings.ReplaceAll(cell.PlainText, "\n", "<br>"))
			m.markdownAnnotation(env, cell, false)
		}

		env.b.WriteString(" ")
	}

	env.b.WriteString("|\n")

	if env.index == 0 {
		for i := 0; i < len(block.Cells); i++ {
			env.b.WriteString("| --- ")
		}

		env.b.WriteString("|\n")
	}
}

func (m *Markdown) markdownLinkPreview(env *markdownEnv, block *notion.LinkPreviewBlock) {
	// TODO
}

func (m *Markdown) markdownLinkToPage(env *markdownEnv, block *notion.LinkToPageBlock) {
	// TODO
}

// TODO handle synced block -> create a separate page and use page embed?
// https://help.obsidian.md/Linking+notes+and+files/Internal+links
func (m *Markdown) markdownSyncedBlock(env *markdownEnv, block *notion.SyncedBlock) {
	if block.SyncedFrom != nil { // TODO handle synced from
		m.markdownSyncedFromBlock(env, block)
		return
	}

	if !m.config.PlainText {
		env.b.WriteString(env.indent)
		env.b.WriteString("<sync ID=\"")
		env.b.WriteString(block.ID())
		env.b.WriteString("\">\n\n")
	}

	m.markdownPlainChildren(env, block)

	if !m.config.PlainText {
		env.b.WriteString(env.indent)
		env.b.WriteString("</sync>\n\n")
	}
}

func (m *Markdown) markdownSyncedFromBlock(env *markdownEnv, block *notion.SyncedBlock) {
	m.loadChildren(block.SyncedFrom.BlockID)

	blocks, err := m.getChildren(block.SyncedFrom.BlockID)
	if err != nil {
		log.Printf("Error fetch children of id: %v, page: %+v, err: %v", block.ID(), block.Parent(), err)
	}

	newEnv := env.Copy()
	newEnv.prev = block
	newEnv.parent = block

	if !m.config.PlainText {
		env.b.WriteString(env.indent)
		env.b.WriteString("<sync sourceID=\"")
		env.b.WriteString(block.SyncedFrom.BlockID)
		env.b.WriteString("\">\n\n")
	}

	m.transformBlocks(newEnv, blocks)

	if !m.config.PlainText {
		env.b.WriteString(env.indent)
		env.b.WriteString("</sync>\n\n")
	}
}
