package transformer

import (
	"log"
	"strings"

	"github.com/dstotijn/go-notion"
)

type markdownBlockWriter func(*markdownEnv, notion.Block)

// https://developers.notion.com/reference/block
var markdownBlockMapper = map[notion.BlockType]markdownBlockWriter{
	notion.BlockTypeParagraph:        markdownParagraph,
	notion.BlockTypeHeading1:         markdownHeading1,
	notion.BlockTypeHeading2:         markdownHeading2,
	notion.BlockTypeHeading3:         markdownHeading3,
	notion.BlockTypeBulletedListItem: markdownBulletedListItem,
	notion.BlockTypeNumberedListItem: markdownNumberedListItem,
	notion.BlockTypeToDo:             markdownToDo,
	notion.BlockTypeToggle:           markdownToggle,
	//notion.BlockTypeChildPage        BlockType = "child_page"
	//notion.BlockTypeChildDatabase    BlockType = "child_database"
	notion.BlockTypeCallout: markdownCallout,
	notion.BlockTypeQuote:   markdownQuote,
	notion.BlockTypeCode:    markdownCode,
	//notion.BlockTypeEmbed            BlockType = "embed"
	notion.BlockTypeImage: markdownImage,
	notion.BlockTypeVideo: markdownVideo,
	//notion.BlockTypeFile             BlockType = "file"
	//notion.BlockTypePDF              BlockType = "pdf"
	notion.BlockTypeBookmark: markdownBookmark,
	notion.BlockTypeEquation: markdownEquation,
	notion.BlockTypeDivider:  markdownDivider,
	//notion.BlockTypeTableOfContents  BlockType = "table_of_contents"
	//notion.BlockTypeBreadCrumb       BlockType = "breadcrumb"
	notion.BlockTypeColumnList:  markdownColumnList,
	notion.BlockTypeColumn:      markdownColumn,
	notion.BlockTypeTable:       markdownTable,
	notion.BlockTypeTableRow:    markdownTableRow,
	notion.BlockTypeLinkPreview: markdownLinkPreview,
	notion.BlockTypeLinkToPage:  markdownLinkToPage,
	notion.BlockTypeSyncedBlock: markdownSyncedBlock,
	//notion.BlockTypeTemplate         BlockType = "template"
	//notion.BlockTypeUnsupported      BlockType = "unsupported"
}

func (m *Markdown) transformBlocks(env *markdownEnv, blocks []notion.Block) {
	// preload children (have a better place to put this?)
	for _, block := range blocks {
		if block.HasChildren {
			parentID := GetParentID(block)
			// check whether it is already loaded before
			if _, ok := m.children[parentID]; ok {
				continue
			}

			block := NewBlockFuture(parentID)
			m.children[parentID] = block
			m.queryChan <- block
		}
	}

	// transform blocks
	for i, block := range blocks {
		writer, ok := markdownBlockMapper[block.Type]
		if !ok {
			continue
		}

		// TODO consider to move special cases to internal parent writer?
		if env.prev != nil {
			if block.Type == notion.BlockTypeSyncedBlock || block.Type == notion.BlockTypeTableRow {
				// skip it
			} else if m.isListType(env.prev.Type) {
				if !m.isListType(block.Type) {
					env.b.WriteString("\n")
				}
			} else {
				env.b.WriteString("\n")
			}
		}

		env.index = i
		writer(env, block)

		if block.HasChildren {
			nEnv := env.Copy()
			// update references (TODO special handle for synced block?)
			nEnv.prev = &block
			nEnv.parent = &block
			// indent if the current block is a list item
			if m.isListType(block.Type) {
				nEnv.indent = nEnv.indent + "  "
			} else if block.Type == notion.BlockTypeToDo {
				nEnv.indent = nEnv.indent + "  "
			}

			parentID := GetParentID(block)
			childBlocks, err := m.getChildren(parentID)
			if err != nil {
				log.Printf("Error fetch children, id: %v, parent id: %v, err: %v", block.ID, parentID, err)
				continue
			}

			m.transformBlocks(nEnv, childBlocks)
		}

		env.prev = &block
	}
}

func markdownAnnotation(env *markdownEnv, text notion.RichText, prefix bool) {
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
			env.b.WriteString(SimpleID(text.Mention.Page.ID))
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

func markdownParagraph(env *markdownEnv, block notion.Block) {
	for _, text := range block.Paragraph.Text {
		markdownAnnotation(env, text, true)
		env.b.WriteString(text.PlainText)
		markdownAnnotation(env, text, false)
	}
	env.b.WriteString("\n")
}

func markdownHeading1(env *markdownEnv, block notion.Block) {
	env.b.WriteString(env.indent)
	env.b.WriteString("## ")
	for _, text := range block.Heading1.Text {
		env.b.WriteString(text.PlainText)
	}
	env.b.WriteString("\n")
}

func markdownHeading2(env *markdownEnv, block notion.Block) {
	env.b.WriteString(env.indent)
	env.b.WriteString("### ")
	for _, text := range block.Heading2.Text {
		env.b.WriteString(text.PlainText)
	}
	env.b.WriteString("\n")
}

func markdownHeading3(env *markdownEnv, block notion.Block) {
	env.b.WriteString(env.indent)
	env.b.WriteString("#### ")
	for _, text := range block.Heading3.Text {
		env.b.WriteString(text.PlainText)
	}
	env.b.WriteString("\n")
}

func markdownBulletedListItem(env *markdownEnv, block notion.Block) {
	env.b.WriteString(env.indent)
	env.b.WriteString("- ")
	for _, text := range block.BulletedListItem.Text {
		markdownAnnotation(env, text, true)
		env.b.WriteString(text.PlainText)
		markdownAnnotation(env, text, false)
	}
	env.b.WriteString("\n")
}

func markdownNumberedListItem(env *markdownEnv, block notion.Block) {
	env.b.WriteString(env.indent)
	env.b.WriteString("0. ")
	for _, text := range block.NumberedListItem.Text {
		markdownAnnotation(env, text, true)
		env.b.WriteString(text.PlainText)
		markdownAnnotation(env, text, false)
	}
	env.b.WriteString("\n")
}

func markdownToDo(env *markdownEnv, block notion.Block) {
	env.b.WriteString(env.indent)

	if *block.ToDo.Checked {
		env.b.WriteString("- [x] ")
	} else {
		env.b.WriteString("- [ ] ")
	}

	for _, text := range block.ToDo.Text {
		markdownAnnotation(env, text, true)
		env.b.WriteString(text.PlainText)
		markdownAnnotation(env, text, false)
	}

	env.b.WriteString("\n")
}

func markdownToggle(env *markdownEnv, block notion.Block) {
	env.b.WriteString(env.indent)

	for _, text := range block.Toggle.Text {
		markdownAnnotation(env, text, true)
		env.b.WriteString(text.PlainText)
		markdownAnnotation(env, text, false)
	}

	env.b.WriteString("\n")
}

func markdownCallout(env *markdownEnv, block notion.Block) {
	env.b.WriteString(env.indent)
	env.b.WriteString("> ")

	for _, text := range block.Callout.Text {
		markdownAnnotation(env, text, true)
		env.b.WriteString(text.PlainText)
		markdownAnnotation(env, text, false)
	}

	env.b.WriteString("\n")
}

func markdownQuote(env *markdownEnv, block notion.Block) {
	env.b.WriteString(env.indent)
	env.b.WriteString("> ")

	for _, text := range block.Quote.Text {
		markdownAnnotation(env, text, true)
		env.b.WriteString(text.PlainText)
		markdownAnnotation(env, text, false)
	}

	env.b.WriteString("\n")
}

func markdownCode(env *markdownEnv, block notion.Block) {
	env.b.WriteString(env.indent)
	env.b.WriteString("``` ")

	if block.Code.Language != nil {
		env.b.WriteString(*block.Code.Language)
	}
	env.b.WriteString("\n")

	for _, text := range block.Code.Text {
		env.b.WriteString(text.PlainText)
	}

	env.b.WriteString("\n```\n")
}

func markdownImage(env *markdownEnv, block notion.Block) {
	var filename string
	var err error

	if block.Image.Type == notion.FileTypeExternal {
		filename = block.Image.External.URL
	} else {
		asset := NewAssetFuture(block.ID, block.Image.File.URL)
		env.m.assetChan <- asset

		filename, err = asset.Read()
	}

	if err != nil {
		return
	}

	env.b.WriteString(env.indent)
	env.b.WriteString("![")

	for _, text := range block.Image.Caption {
		env.b.WriteString(text.PlainText)
	}

	env.b.WriteString("](")
	env.b.WriteString(filename)
	env.b.WriteString(")\n")
}

func markdownVideo(env *markdownEnv, block notion.Block) {
	env.b.WriteString(env.indent)
	env.b.WriteString("[")

	for _, text := range block.Video.Caption {
		env.b.WriteString(text.PlainText)
	}

	env.b.WriteString("](")

	if block.Video.Type == notion.FileTypeExternal {
		env.b.WriteString(block.Video.External.URL)
	} else {
		env.b.WriteString(block.Video.File.URL) // TODO downlaod
	}

	env.b.WriteString(")\n")
}

func markdownBookmark(env *markdownEnv, block notion.Block) {
	env.b.WriteString(env.indent)

	hasCaption := len(block.Bookmark.Caption) > 0

	if hasCaption {
		env.b.WriteString("[")
		for _, text := range block.Bookmark.Caption {
			env.b.WriteString(text.PlainText)
		}
		env.b.WriteString("](")
	}

	env.b.WriteString(block.Bookmark.URL)

	if hasCaption {
		env.b.WriteString(")")
	}

	env.b.WriteString("\n")
}

func markdownEquation(env *markdownEnv, block notion.Block) {
	env.b.WriteString(env.indent)
	env.b.WriteString("$$")
	env.b.WriteString(block.Equation.Expression)
	env.b.WriteString("$$\n")
}

func markdownDivider(env *markdownEnv, block notion.Block) {
	env.b.WriteString(env.indent)
	env.b.WriteString("---\n")
}

func markdownColumnList(env *markdownEnv, block notion.Block) {
	// Empty
}

func markdownColumn(env *markdownEnv, block notion.Block) {
	// Empty
}

func markdownTable(env *markdownEnv, block notion.Block) {
	// Empty
}

func markdownTableRow(env *markdownEnv, block notion.Block) {
	for _, cells := range block.TableRow.Cells {
		env.b.WriteString("| ")

		for _, cell := range cells {
			markdownAnnotation(env, cell, true)
			env.b.WriteString(strings.ReplaceAll(cell.PlainText, "\n", "<br>"))
			markdownAnnotation(env, cell, true)
		}

		env.b.WriteString(" ")
	}

	env.b.WriteString("|\n")

	if env.index == 0 {
		for i := 0; i < len(block.TableRow.Cells); i++ {
			env.b.WriteString("| --- ")
		}

		env.b.WriteString("|\n")
	}
}

func markdownLinkPreview(env *markdownEnv, block notion.Block) {
	// TODO
}

func markdownLinkToPage(env *markdownEnv, block notion.Block) {
	// TODO
}

// TODO handle synced block -> create a separate page and use page embed?
func markdownSyncedBlock(env *markdownEnv, block notion.Block) {
	if block.SyncedBlock.SyncedFrom != nil {
		return // TODO how to do block link?
	}

	// TODO handle synced from
}
