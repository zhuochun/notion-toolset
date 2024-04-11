package main

import (
	"context"
	"errors"
	"fmt"
	"html/template"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/dstotijn/go-notion"
	"github.com/sashabaranov/go-openai"
	"github.com/zhuochun/notion-toolset/transformer"
	"golang.org/x/time/rate"
)

type LangModelConfig struct {
	DatabaseID    string `yaml:"databaseID"`
	DatabaseQuery string `yaml:"databaseQuery"`
	LookbackDays  int    `yaml:"lookbackDays"` // additional date info
	// Read from chain file instead of database, overwrite database configs above
	ChainFile string `yaml:"chainFile"`
	// LLM prompt message
	Prompt        string `yaml:"prompt"`
	Model         string `yaml:"model"`
	RespTextBlock string `yaml:"respTextBlock"` // Format https://pkg.go.dev/github.com/dstotijn/go-notion#ParagraphBlock
	// tuning https://developers.notion.com/reference/request-limits
	TaskSpeed float64 `yaml:"taskSpeed"`
	// skip processing a pages if chars is <min or >max thresholds
	PageMinChars int `yaml:"pageMinChars"`
	PageMaxChars int `yaml:"pageMaxChars"`
}

type LangModel struct {
	DebugMode bool
	ExecOne   string

	Client       *notion.Client
	OpenaiClient *openai.Client

	LangModelConfig

	queryLimiter *rate.Limiter
	taskPool     chan notion.Page
	queryPool    chan *transformer.BlockFuture
}

func (m *LangModel) Validate() error {
	if m.Prompt == "" {
		return errors.Join(ErrConfigRequired, fmt.Errorf("set Prompt"))
	}

	// init OpenAI client
	openaiToken := os.Getenv("DOT_OPENAI_KEY")
	if openaiToken == "" {
		return fmt.Errorf("missing token in env.DOT_OPENAI_KEY")
	}
	m.OpenaiClient = openai.NewClient(openaiToken)

	// set default exportspeed
	if m.TaskSpeed < 1 {
		m.TaskSpeed = 2.8
	} else if m.TaskSpeed > 3 {
		m.TaskSpeed = 3
	}

	return nil
}

func (m *LangModel) Run() error {
	m.queryLimiter = rate.NewLimiter(rate.Limit(m.TaskSpeed), int(m.TaskSpeed))

	// workers to process LLM prompt per page
	taskWg := new(sync.WaitGroup)
	m.taskPool = m.StartLLMTasker(taskWg, int(m.TaskSpeed))
	// workers to query content of notion blocks
	queryWg := new(sync.WaitGroup)
	m.queryPool = m.StartQuerier(queryWg, int(m.TaskSpeed))

	// query database pages, queue each pages for export
	pagesChan, errChan := m.ScanPages()
	pageNum := 0
	for pages := range pagesChan {
		for _, page := range pages {
			pageNum += 1
			m.taskPool <- page

			if m.DebugMode && pageNum%500 == 0 {
				log.Printf("Scanned pages: %v so far", pageNum)
			}
		}
	}
	log.Printf("Scanned pages: %v", pageNum)

	close(m.taskPool) // TODO do not support sub-page now, same as export cmd
	taskWg.Wait()

	close(m.queryPool)
	queryWg.Wait()

	select {
	case err := <-errChan:
		return err
	default:
		return nil
	}
}

func (m *LangModel) ScanPages() (chan []notion.Page, chan error) {
	if m.ExecOne != "" { // exec one page ID
		return m.scanDirectPages([]string{m.ExecOne})
	}

	if m.ChainFile != "" { // exec IDs found in a file
		content, err := os.ReadFile(m.ChainFile)
		if err != nil {
			log.Printf("Open file errored, file: %v, err: %v", m.ChainFile, err)
			return m.scanDirectPages([]string{})
		}

		normalizedContent := strings.Replace(string(content), "\r\n", "\n", -1)
		pageIDs := strings.Split(normalizedContent, "\n")

		return m.scanDirectPages(pageIDs)
	}

	return m.scanDatabasePages()
}

func (m *LangModel) scanDirectPages(pageIDs []string) (chan []notion.Page, chan error) {
	pagesChan := make(chan []notion.Page, len(pageIDs))
	errChan := make(chan error, 1)

	for _, pageID := range pageIDs {
		pageID = transformer.SimpleID(pageID)

		if page, err := m.Client.FindPageByID(context.Background(), pageID); err == nil {
			pagesChan <- []notion.Page{page}
		} else {
			errChan <- err
		}
	}

	close(pagesChan)
	return pagesChan, errChan
}

func (m *LangModel) scanDatabasePages() (chan []notion.Page, chan error) {
	q := NewDatabaseQuery(m.Client, m.DatabaseID)

	date := "" // default
	if m.LookbackDays > 0 {
		date = time.Now().AddDate(0, 0, -m.LookbackDays).Format(layoutDate)
	}

	if err := q.SetQuery(m.DatabaseQuery, QueryBuilder{Date: date}); err != nil {
		log.Panicf("Invalid query: %v, err: %v", m.DatabaseQuery, err)
	}

	if m.DebugMode {
		log.Printf("DatabaseQuery Filter: %+v", q.Query.Filter)
		log.Printf("DatabaseQuery Sorter: %+v", q.Query.Sorts)
	}

	return q.Go(context.Background(), 1, m.queryLimiter)
}

func (m *LangModel) StartQuerier(wg *sync.WaitGroup, size int) chan *transformer.BlockFuture {
	taskPool := make(chan *transformer.BlockFuture, size)

	for i := 0; i < size; i++ {
		wg.Add(1)

		go func() {
			for task := range taskPool {
				blocks, err := m.QueryBlocks(task.BlockID) // TODO add retry?
				task.Write(blocks, err)
			}
			wg.Done()
		}()
	}

	return taskPool
}

func (m *LangModel) QueryBlocks(blockID string) ([]notion.Block, error) {
	m.queryLimiter.Wait(context.Background())

	blocks := []notion.Block{}
	cursor := ""
	for {
		query := &notion.PaginationQuery{StartCursor: cursor}
		resp, err := m.Client.FindBlockChildrenByID(context.TODO(), blockID, query)
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

func (m *LangModel) StartLLMTasker(wg *sync.WaitGroup, size int) chan notion.Page {
	taskPool := make(chan notion.Page, size)

	for i := 0; i < size; i++ {
		wg.Add(1)

		go func() {
			for page := range taskPool {
				if err := m.runLLMPage(page); err != nil {
					log.Printf("Failed to run LLM: %v", err)
				}
			}

			wg.Done()
		}()
	}

	return taskPool
}

func (m *LangModel) runLLMPage(page notion.Page) error {
	blocks, err := m.QueryBlocks(page.ID)
	if err != nil {
		return fmt.Errorf("query block id: %v, err: %v", page.ID, err)
	}

	markdown := transformer.MarkdownConfig{
		NoAlias:        true,
		NoFrontMatters: true,
		NoMetadata:     true,
		TitleToH1:      true,
		PlainText:      true,
	}

	t := transformer.New(markdown, &page, blocks, m.queryPool, nil)
	content := t.Transform()

	if len(content) < m.PageMinChars {
		log.Printf("Skip content by MinChars=%v, id: %v, len: %v", m.PageMinChars, page.ID, len(content))
		return nil
	} else if m.PageMaxChars > 0 && len(content) > m.PageMaxChars {
		log.Printf("Skip content by MaxChars=%v, id: %v, len: %v", m.PageMaxChars, page.ID, len(content))
		return nil
	}

	req := openai.ChatCompletionRequest{
		Model: openai.GPT3Dot5Turbo,
		Messages: []openai.ChatCompletionMessage{
			{
				Role:    openai.ChatMessageRoleSystem,
				Content: m.Prompt,
			},
			{
				Role:    openai.ChatMessageRoleUser,
				Content: content,
			},
		},
	}

	if m.Model != "" { // Use user requested model
		req.Model = m.Model
	}

	resp, err := m.OpenaiClient.CreateChatCompletion(context.Background(), req)
	if err != nil {
		return fmt.Errorf("openai chat err: %v", err)
	}

	block, err := m.WriteBlock(page, resp.Choices[0].Message.Content)
	if err != nil {
		return err
	}

	if len(block.Results) > 0 {
		log.Printf("Append block child %v", block.Results[0].ID())
	}
	return nil
}

func (m *LangModel) WriteBlock(page notion.Page, content string) (notion.BlockChildrenResponse, error) {
	w := NewAppendBlock(m.Client, page.ID)

	paragraphs := strings.Split(content, "\n")

	for _, p := range paragraphs {
		if p == "" { // skip empty lines
			continue
		}

		p = strings.TrimPrefix(p, "- ") // TODO better handle response text

		if m.RespTextBlock != "" {
			if err := w.AddParagraph("LLM", m.RespTextBlock, BlockBuilder{
				Date:    time.Now().Format(layoutDate),
				Content: template.HTMLEscaper(p),
			}); err != nil {
				return notion.BlockChildrenResponse{}, err
			}
		} else {
			w.Blocks = append(w.Blocks, notion.ParagraphBlock{
				RichText: []notion.RichText{
					{Text: &notion.Text{Content: p}},
				},
			})
		}
	}

	return w.Do(context.TODO())
}
