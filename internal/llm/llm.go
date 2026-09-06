package llm

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/dstotijn/go-notion"
	"github.com/sashabaranov/go-openai"
	"github.com/zhuochun/notion-toolset/internal/config"
	"github.com/zhuochun/notion-toolset/internal/notionops"
	"github.com/zhuochun/notion-toolset/notionread"
	"github.com/zhuochun/notion-toolset/transformer"
)

type LangModel struct {
	Now         func() time.Time
	Logger      *log.Logger
	SetupClient func() (*openai.Client, error)
	DebugMode   bool
	ExecOne     string

	Client       *notion.Client
	OpenaiClient *openai.Client

	config.LangModelConfig

	notionReader *notionread.Reader
}

func (m *LangModel) Validate() error {
	if m.Prompt == "" {
		return errors.Join(config.ErrRequired, fmt.Errorf("set Prompt"))
	}

	if m.SetupClient != nil {
		client, err := m.SetupClient()
		if err != nil {
			return err
		}
		m.OpenaiClient = client
	} else if m.OpenaiClient == nil {
		return fmt.Errorf("missing LLM client setup")
	}

	if m.TaskSpeed < 1 {
		m.TaskSpeed = 2.8
	} else if m.TaskSpeed > 3 {
		m.TaskSpeed = 3
	}

	return nil
}

func (m *LangModel) Run() error {
	if m.GroupExec {
		return m.runLLMGroup()
	}

	m.notionReader = notionops.NewReader(m.Client, m.TaskSpeed)

	taskWg := new(sync.WaitGroup)
	taskPool := m.startLLMTasker(taskWg, int(m.TaskSpeed))

	pageNum := 0
	scanErr := m.ScanPages(context.Background(), func(page notion.Page) error {
		pageNum++
		taskPool <- page
		if m.DebugMode && pageNum%500 == 0 {
			m.logger().Printf("Scanned pages: %v so far", pageNum)
		}
		return nil
	})
	m.logger().Printf("Scanned pages: %v", pageNum)

	close(taskPool)
	taskWg.Wait()

	return scanErr
}

func (m *LangModel) ScanPages(ctx context.Context, visit func(notion.Page) error) error {
	if m.ExecOne != "" {
		return m.scanDirectPages(ctx, []string{m.ExecOne}, visit)
	}

	if m.ChainFile != "" {
		content, err := os.ReadFile(m.ChainFile)
		if err != nil {
			m.logger().Printf("Open file errored, file: %v, err: %v", m.ChainFile, err)
			return nil
		}

		normalizedContent := strings.Replace(string(content), "\r\n", "\n", -1)
		pageIDs := strings.Split(normalizedContent, "\n")

		return m.scanDirectPages(ctx, pageIDs, visit)
	}

	return m.scanDatabasePages(ctx, visit)
}

func (m *LangModel) scanDirectPages(ctx context.Context, pageIDs []string, visit func(notion.Page) error) error {
	var errs []error
	for _, pageID := range pageIDs {
		if pageID == "" {
			continue
		}
		pageID = transformer.SimpleID(pageID)

		if page, err := m.reader().Page(ctx, pageID); err == nil {
			if err := visit(page); err != nil {
				return err
			}
		} else {
			errs = append(errs, fmt.Errorf("find page by id %v: %w", pageID, err))
		}
	}

	return errors.Join(errs...)
}

func (m *LangModel) scanDatabasePages(ctx context.Context, visit func(notion.Page) error) error {
	q := notionops.NewDatabaseQuery(m.Client, m.DatabaseID)

	today := m.now().Format(notionops.DateLayout)
	date := ""
	if m.LookbackDays > 0 {
		date = m.now().AddDate(0, 0, -m.LookbackDays).Format(notionops.DateLayout)
	}

	if err := q.SetQuery(m.DatabaseQuery, notionops.QueryBuilder{Date: date, Today: today}); err != nil {
		m.logger().Panicf("Invalid query: %v, err: %v", m.DatabaseQuery, err)
	}

	if m.DebugMode {
		m.logger().Printf("DatabaseQuery Filter: %+v", q.Query.Filter)
		m.logger().Printf("DatabaseQuery Sorter: %+v", q.Query.Sorts)
	}

	return q.ForEach(ctx, 0, m.reader(), visit)
}

func (m *LangModel) startLLMTasker(wg *sync.WaitGroup, size int) chan notion.Page {
	taskPool := make(chan notion.Page, size)

	for i := 0; i < size; i++ {
		wg.Add(1)

		go func() {
			for page := range taskPool {
				if err := m.runLLMPage(page); err != nil {
					m.logger().Printf("Failed to run LLM: %v", err)
				}
			}

			wg.Done()
		}()
	}

	return taskPool
}

func (m *LangModel) runLLMGroup() error {
	m.notionReader = notionops.NewReader(m.Client, m.TaskSpeed)

	pages := []notion.Page{}
	err := m.ScanPages(context.Background(), func(page notion.Page) error {
		pages = append(pages, page)
		return nil
	})
	m.logger().Printf("Scanned pages: %v", len(pages))
	if err != nil {
		return err
	}

	var contents []string
	for _, page := range pages {
		content, eligible, err := m.pageContent(page)
		if err != nil {
			return err
		}
		if eligible {
			contents = append(contents, content)
		}
	}

	if len(contents) == 0 {
		return nil
	}

	target := notion.Page{}
	if m.GroupJournalID != "" {
		p, err := m.getJournalPage()
		if err != nil {
			return err
		}
		target = p
	}
	if target.ID == "" && m.ExecOne != "" {
		p, err := m.reader().Page(context.Background(), transformer.SimpleID(m.ExecOne))
		if err == nil {
			target = p
		}
	}
	if target.ID == "" {
		target = pages[len(pages)-1]
	}

	content := strings.Join(contents, "\n")
	return m.runLLMContent(target, content)
}

func (m *LangModel) reader() *notionread.Reader {
	if m.notionReader != nil {
		return m.notionReader
	}
	return notionread.New(m.Client, notionread.WithConcurrency(max(1, int(m.TaskSpeed))))
}

func (m *LangModel) runLLMPage(page notion.Page) error {
	content, eligible, err := m.pageContent(page)
	if err != nil || !eligible {
		return err
	}
	return m.runLLMContent(page, content)
}

// pageContent keeps rendering and byte-length eligibility identical in both modes.
// Eligibility is explicit so an empty result is not confused with a skipped page.
func (m *LangModel) pageContent(page notion.Page) (string, bool, error) {
	snapshot, err := m.reader().BlockSnapshot(context.TODO(), page.ID, notionread.BestEffort)
	if err != nil {
		return "", false, fmt.Errorf("query block id: %v, err: %v", page.ID, err)
	}

	markdown := transformer.MarkdownConfig{
		NoAlias:        true,
		NoFrontMatters: true,
		NoMetadata:     true,
		TitleToH1:      true,
		PlainText:      true,
	}

	t := transformer.New(markdown, &page, snapshot, nil)
	content := t.Transform()

	if len(content) < m.PageMinChars {
		m.logger().Printf("Skip content by MinChars=%v, id: %v, len: %v", m.PageMinChars, page.ID, len(content))
		return "", false, nil
	} else if m.PageMaxChars > 0 && len(content) > m.PageMaxChars {
		m.logger().Printf("Skip content by MaxChars=%v, id: %v, len: %v", m.PageMaxChars, page.ID, len(content))
		return "", false, nil
	}

	return content, true, nil
}

func (m *LangModel) runLLMContent(page notion.Page, content string) error {
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

	if m.Model != "" {
		req.Model = m.Model
	}

	if m.Temperature != nil {
		req.Temperature = *m.Temperature
	}

	if m.RespJSON {
		req.ResponseFormat = &openai.ChatCompletionResponseFormat{
			Type: openai.ChatCompletionResponseFormatTypeJSONObject,
		}
	}

	resp, err := m.OpenaiClient.CreateChatCompletion(context.Background(), req)
	if err != nil {
		return fmt.Errorf("openai chat err: %v", err)
	}

	block := notion.BlockChildrenResponse{}
	if m.RespJSON {
		block, err = m.WriteJSON(page, resp.Choices[0].Message.Content)
	} else {
		block, err = m.WriteBlock(page, resp.Choices[0].Message.Content)
	}
	if err != nil {
		return err
	}
	if len(block.Results) > 0 {
		m.logger().Printf("Append block child %v", block.Results[0].ID())
	}
	return nil
}

func (m *LangModel) getJournalPage() (notion.Page, error) {
	now := m.now()
	title := now.Format(notionops.DateLayout)

	q := notionops.NewDatabaseQuery(m.Client, m.GroupJournalID)
	q.Query = &notion.DatabaseQuery{
		Filter: &notion.DatabaseQueryFilter{
			Property: "title",
			DatabaseQueryPropertyFilter: notion.DatabaseQueryPropertyFilter{
				Title: &notion.TextPropertyFilter{Equals: title},
			},
		},
		Sorts: []notion.DatabaseQuerySort{
			{Timestamp: notion.SortTimeStampCreatedTime, Direction: notion.SortDirAsc},
		},
	}

	pages, err := q.Once(context.TODO())
	if err != nil {
		return notion.Page{}, fmt.Errorf("no journal found: %v, err: %v", title, err)
	}
	if len(pages) == 0 {
		return notion.Page{}, fmt.Errorf("no journal found: %v", title)
	}
	if len(pages) > 1 {
		m.logger().Printf("Multiple journal found: %v, cnt: %v, uses: %v", title, len(pages), pages[0].ID)
	}
	if m.DebugMode {
		m.logger().Printf("Journal by title: %v, found: %v, uses: %v", title, len(pages), pages[0].ID)
	}

	return pages[0], nil
}

func (m *LangModel) logger() *log.Logger {
	if m.Logger != nil {
		return m.Logger
	}
	return log.Default()
}

func (m *LangModel) now() time.Time {
	if m.Now != nil {
		return m.Now()
	}
	return time.Now()
}
