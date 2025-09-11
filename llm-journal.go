package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"log"
	"os"
	"strings"
	"time"

	"github.com/dstotijn/go-notion"
	"github.com/sashabaranov/go-openai"
	"github.com/zhuochun/notion-toolset/transformer"
)

// LLMJournalConfig holds configuration for llm-journal command.
type LLMJournalConfig struct {
	DailyDatabaseID  string `yaml:"dailyDatabaseID"`
	WeeklyDatabaseID string `yaml:"weeklyDatabaseID"`
	MemoryPageID     string `yaml:"memoryPageID"`
	MemoryPrompt     string `yaml:"memoryPrompt"`

	Prompt      string   `yaml:"prompt"`
	Model       string   `yaml:"model"`
	Temperature *float32 `yaml:"temperature"`

	RespJSON      bool   `yaml:"respJSON"`
	RespTextBlock string `yaml:"respTextBlock"`
}

// LLMJournal summarises journal pages with LLM.
type LLMJournal struct {
	DebugMode bool

	Client       *notion.Client
	OpenaiClient *openai.Client

	LLMJournalConfig
}

func (l *LLMJournal) Validate() error {
	if l.Prompt == "" {
		return errors.Join(ErrConfigRequired, fmt.Errorf("set prompt"))
	}

	openaiToken := os.Getenv("DOT_OPENAI_KEY")
	if openaiToken == "" {
		return fmt.Errorf("missing token in env.DOT_OPENAI_KEY")
	}
	l.OpenaiClient = openai.NewClient(openaiToken)
	return nil
}

func (l *LLMJournal) Run() error {
	start, end := l.weekRange(time.Now())

	dailyContent, err := l.fetchDailyContent(start)
	if err != nil {
		return err
	}

	prevWeekly, err := l.fetchPrevWeekly(start)
	if err != nil {
		return err
	}

	memoryContent := ""
	if l.MemoryPageID != "" {
		memoryContent, _ = l.pageContent(l.MemoryPageID)
	}

	var builder strings.Builder
	if memoryContent != "" {
		builder.WriteString("Memory:\n")
		builder.WriteString(memoryContent)
		builder.WriteString("\n\n")
	}
	for i, c := range prevWeekly {
		builder.WriteString(fmt.Sprintf("Previous Week %d:\n", len(prevWeekly)-i))
		builder.WriteString(c)
		builder.WriteString("\n\n")
	}
	builder.WriteString("Daily Journals:\n")
	builder.WriteString(dailyContent)

	ctx := builder.String()
	summary, err := l.runLLM(l.Prompt, ctx)
	if err != nil {
		return err
	}

	weekTitle := fmt.Sprintf("%s/%s", start.Format(layoutDate), end.Format(layoutDate))
	page, err := l.getPageByTitle(l.WeeklyDatabaseID, weekTitle)
	if err != nil {
		return err
	}

	if l.RespJSON {
		_, err = l.writeJSON(page, summary)
	} else {
		_, err = l.writeBlock(page, summary)
	}
	if err != nil {
		return err
	}

	if l.MemoryPageID != "" && l.MemoryPrompt != "" {
		memCtx := memoryContent + "\n\n" + summary
		mem, err := l.runLLM(l.MemoryPrompt, memCtx)
		if err == nil {
			mPage := notion.Page{ID: l.MemoryPageID}
			if l.RespJSON {
				l.writeJSON(mPage, mem)
			} else {
				l.writeBlock(mPage, mem)
			}
		} else {
			log.Printf("memory update failed: %v", err)
		}
	}

	return nil
}

func (l *LLMJournal) weekRange(now time.Time) (time.Time, time.Time) {
	weekday := now.Weekday()
	if weekday == time.Sunday {
		weekday = 7
	}
	monday := time.Date(now.Year(), now.Month(), now.Day()-int(weekday-1), 0, 0, 0, 0, now.Location())
	if weekday < time.Saturday { // Mon-Fri use previous week
		monday = monday.AddDate(0, 0, -7)
	}
	sunday := monday.AddDate(0, 0, 6)
	return monday, sunday
}

func (l *LLMJournal) fetchDailyContent(start time.Time) (string, error) {
	var out strings.Builder
	for i := 0; i < 7; i++ {
		d := start.AddDate(0, 0, i)
		title := d.Format(layoutDate)
		page, err := l.getPageByTitle(l.DailyDatabaseID, title)
		if err != nil {
			continue
		}
		content, err := l.pageContent(page.ID)
		if err != nil {
			return "", err
		}
		out.WriteString(title + "\n" + content + "\n\n")
	}
	return strings.TrimSpace(out.String()), nil
}

func (l *LLMJournal) fetchPrevWeekly(start time.Time) ([]string, error) {
	var contents []string
	for i := 1; i <= 3; i++ {
		s := start.AddDate(0, 0, -7*i)
		e := s.AddDate(0, 0, 6)
		title := fmt.Sprintf("%s/%s", s.Format(layoutDate), e.Format(layoutDate))
		page, err := l.getPageByTitle(l.WeeklyDatabaseID, title)
		if err != nil {
			continue
		}
		c, err := l.pageContent(page.ID)
		if err != nil {
			return contents, err
		}
		contents = append(contents, c)
	}
	return contents, nil
}

func (l *LLMJournal) getPageByTitle(databaseID, title string) (notion.Page, error) {
	q := NewDatabaseQuery(l.Client, databaseID)
	q.Query = &notion.DatabaseQuery{
		Filter: &notion.DatabaseQueryFilter{
			Property: "title",
			DatabaseQueryPropertyFilter: notion.DatabaseQueryPropertyFilter{
				Title: &notion.TextPropertyFilter{Equals: title},
			},
		},
		Sorts:    []notion.DatabaseQuerySort{{Timestamp: notion.SortTimeStampCreatedTime, Direction: notion.SortDirAsc}},
		PageSize: 1,
	}
	pages, err := q.Once(context.TODO())
	if err != nil || len(pages) == 0 {
		return notion.Page{}, fmt.Errorf("page not found: %v", title)
	}
	return pages[0], nil
}

func (l *LLMJournal) pageContent(pageID string) (string, error) {
	blocks, err := l.queryBlocks(pageID)
	if err != nil {
		return "", err
	}
	mdConf := transformer.MarkdownConfig{
		NoAlias:        true,
		NoFrontMatters: true,
		NoMetadata:     true,
		TitleToH1:      false,
		PlainText:      true,
	}
	t := transformer.New(mdConf, nil, blocks, nil, nil)
	return t.Transform(), nil
}

func (l *LLMJournal) queryBlocks(blockID string) ([]notion.Block, error) {
	blocks := []notion.Block{}
	cursor := ""
	for {
		resp, err := l.Client.FindBlockChildrenByID(context.TODO(), blockID, &notion.PaginationQuery{StartCursor: cursor})
		if err != nil {
			return blocks, err
		}
		blocks = append(blocks, resp.Results...)
		if resp.HasMore {
			cursor = *resp.NextCursor
		} else {
			break
		}
	}
	return blocks, nil
}

func (l *LLMJournal) runLLM(prompt, content string) (string, error) {
	req := openai.ChatCompletionRequest{
		Model: openai.GPT3Dot5Turbo,
		Messages: []openai.ChatCompletionMessage{
			{Role: openai.ChatMessageRoleSystem, Content: prompt},
			{Role: openai.ChatMessageRoleUser, Content: content},
		},
	}
	if l.Model != "" {
		req.Model = l.Model
	}
	if l.Temperature != nil {
		req.Temperature = *l.Temperature
	}
	if l.RespJSON {
		req.ResponseFormat = &openai.ChatCompletionResponseFormat{Type: openai.ChatCompletionResponseFormatTypeJSONObject}
	}
	resp, err := l.OpenaiClient.CreateChatCompletion(context.Background(), req)
	if err != nil {
		return "", fmt.Errorf("openai chat err: %w", err)
	}
	return resp.Choices[0].Message.Content, nil
}

func (l *LLMJournal) writeBlock(page notion.Page, content string) (notion.BlockChildrenResponse, error) {
	w := NewAppendBlock(l.Client, page.ID)
	paragraphs := strings.Split(content, "\n")
	for _, p := range paragraphs {
		if p == "" {
			continue
		}
		p = strings.TrimPrefix(p, "- ")
		if l.RespTextBlock != "" {
			if err := w.AddParagraph("LLMJournal", l.RespTextBlock, BlockBuilder{
				Date:    time.Now().Format(layoutDate),
				Content: template.HTMLEscaper(p),
			}); err != nil {
				return notion.BlockChildrenResponse{}, err
			}
		} else {
			w.Blocks = append(w.Blocks, notion.ParagraphBlock{
				RichText: []notion.RichText{{Text: &notion.Text{Content: p}}},
			})
		}
	}
	return w.Do(context.TODO())
}

func (l *LLMJournal) writeJSON(page notion.Page, content string) (notion.BlockChildrenResponse, error) {
	w := NewAppendBlock(l.Client, page.ID)
	data := map[string]interface{}{}
	if err := json.Unmarshal([]byte(content), &data); err != nil {
		return notion.BlockChildrenResponse{}, fmt.Errorf("unmarshal content: %w", err)
	}
	if err := w.AddBlocks("llmJournalJSON", l.RespTextBlock, data); err != nil {
		return notion.BlockChildrenResponse{}, err
	}
	return w.Do(context.TODO())
}
