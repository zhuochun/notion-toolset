package transformer

import (
	"fmt"
	"strings"

	"github.com/dstotijn/go-notion"
)

func SimpleID(id string) string {
	return strings.ReplaceAll(id, "-", "")
}

func GetPageTitle(page notion.Page) (string, error) {
	switch props := page.Properties.(type) {
	case notion.PageProperties:
		return concatRichText(props.Title.Title), nil
	case notion.DatabasePageProperties:
		for _, prop := range props {
			if prop.ID == "title" {
				return concatRichText(prop.Title), nil
			}
		}
		return "", fmt.Errorf("no title properties in database page properties")
	default:
		return "", fmt.Errorf("invalid notion page properties")
	}
}

func concatRichText(blocks []notion.RichText) string {
	if len(blocks) == 0 {
		return ""
	} else if len(blocks) == 1 {
		return blocks[0].PlainText
	}

	title := blocks[0].PlainText
	for i := 1; i < len(blocks); i++ {
		title += blocks[i].PlainText
	}
	return title
}

func GetParentID(block notion.Block) string {
	if block.Type == notion.BlockTypeSyncedBlock && block.SyncedBlock.SyncedFrom != nil {
		if block.SyncedBlock.SyncedFrom.Type == notion.SyncedFromTypeBlockID {
			return block.SyncedBlock.SyncedFrom.BlockID
		}
	}

	return block.ID
}
