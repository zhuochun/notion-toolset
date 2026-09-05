package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"strings"
	"time"

	"github.com/dstotijn/go-notion"
	"github.com/zhuochun/notion-toolset/internal/notionops"
)

func (m *LangModel) WriteBlock(page notion.Page, content string) (notion.BlockChildrenResponse, error) {
	w := notionops.NewAppendBlock(m.Client, page.ID)

	paragraphs := strings.Split(content, "\n")

	for _, p := range paragraphs {
		if p == "" {
			continue
		}

		p = strings.TrimPrefix(p, "- ")

		if m.RespTextBlock != "" {
			if err := w.AddParagraph("LLM", m.RespTextBlock, notionops.BlockBuilder{
				Date:    time.Now().Format(notionops.DateLayout),
				Content: template.HTMLEscaper(p),
			}); err != nil {
				return notion.BlockChildrenResponse{}, err
			}
		} else {
			w.Blocks = append(w.Blocks, &notion.ParagraphBlock{
				RichText: []notion.RichText{
					{Text: &notion.Text{Content: p}},
				},
			})
		}
	}

	return w.Do(context.TODO())
}

func (m *LangModel) WriteJSON(page notion.Page, content string) (notion.BlockChildrenResponse, error) {
	w := notionops.NewAppendBlock(m.Client, page.ID)

	contentJSON := map[string]interface{}{}
	if err := json.Unmarshal([]byte(content), &contentJSON); err != nil {
		return notion.BlockChildrenResponse{}, fmt.Errorf("unmarshal content: %w", err)
	}

	if err := w.AddBlocks("llmJSON", m.RespTextBlock, contentJSON); err != nil {
		return notion.BlockChildrenResponse{}, err
	}

	return w.Do(context.TODO())
}
