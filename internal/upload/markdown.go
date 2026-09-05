package upload

import (
	"path/filepath"
	"regexp"
	"strings"

	"github.com/dstotijn/go-notion"
)

var hashIDRegex = regexp.MustCompile("([a-zA-Z0-9]{32})")

func (r *ReverseUploader) uploadBody(local []byte) (string, string) {
	body := stripFrontMatter(string(local))
	if r.Markdown.TitleToH1 {
		return stripLeadingH1(body)
	}
	return "", body
}

func extractNotionPageID(filename string, content []byte) string {
	if match := hashIDRegex.FindString(filepath.Base(filename)); match != "" {
		return match
	}
	frontMatter := frontMatter(content)
	if frontMatter == "" {
		return ""
	}
	return hashIDRegex.FindString(frontMatter)
}

func frontMatter(content []byte) string {
	s := normalizeEOL(string(content))
	if !strings.HasPrefix(s, "---\n") {
		return ""
	}
	rest := s[len("---\n"):]
	end := strings.Index(rest, "\n---")
	if end == -1 {
		return ""
	}
	return rest[:end]
}

func stripFrontMatter(content string) string {
	s := normalizeEOL(content)
	if !strings.HasPrefix(s, "---\n") {
		return s
	}
	rest := s[len("---\n"):]
	end := strings.Index(rest, "\n---")
	if end == -1 {
		return s
	}
	after := rest[end+len("\n---"):]
	return strings.TrimLeft(after, "\n")
}

func stripLeadingH1(content string) (string, string) {
	lines := strings.Split(normalizeEOL(content), "\n")
	for i, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if strings.HasPrefix(line, "# ") {
			title := strings.TrimSpace(strings.TrimPrefix(line, "# "))
			body := strings.Join(lines[i+1:], "\n")
			return title, strings.TrimLeft(body, "\n")
		}
		return "", content
	}
	return "", content
}

func markdownToNotionBlocks(content string) []notion.Block {
	lines := strings.Split(normalizeEOL(content), "\n")
	var blocks []notion.Block
	var paragraph []string
	var code []string
	var codeLang string
	inCode := false

	flushParagraph := func() {
		if len(paragraph) == 0 {
			return
		}
		text := strings.Join(paragraph, "\n")
		blocks = append(blocks, &notion.ParagraphBlock{RichText: richTextChunks(text)})
		paragraph = nil
	}
	flushCode := func() {
		text := strings.Join(code, "\n")
		lang := codeLang
		if lang == "" {
			lang = "plain text"
		}
		blocks = append(blocks, &notion.CodeBlock{RichText: richTextChunks(text), Language: &lang})
		code = nil
		codeLang = ""
	}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") {
			if inCode {
				flushCode()
				inCode = false
			} else {
				flushParagraph()
				inCode = true
				codeLang = strings.TrimSpace(strings.TrimPrefix(trimmed, "```"))
			}
			continue
		}
		if inCode {
			code = append(code, line)
			continue
		}
		if trimmed == "" {
			flushParagraph()
			continue
		}
		if block, ok := markdownLineBlock(line); ok {
			flushParagraph()
			blocks = append(blocks, block)
			continue
		}
		paragraph = append(paragraph, line)
	}
	if inCode {
		flushCode()
	}
	flushParagraph()
	return blocks
}

func markdownLineBlock(line string) (notion.Block, bool) {
	trimmed := strings.TrimSpace(line)
	switch {
	case strings.HasPrefix(trimmed, "### "):
		return &notion.Heading3Block{RichText: richTextChunks(strings.TrimSpace(strings.TrimPrefix(trimmed, "### ")))}, true
	case strings.HasPrefix(trimmed, "## "):
		return &notion.Heading2Block{RichText: richTextChunks(strings.TrimSpace(strings.TrimPrefix(trimmed, "## ")))}, true
	case strings.HasPrefix(trimmed, "# "):
		return &notion.Heading1Block{RichText: richTextChunks(strings.TrimSpace(strings.TrimPrefix(trimmed, "# ")))}, true
	case strings.HasPrefix(trimmed, "- [ ] "):
		checked := false
		return &notion.ToDoBlock{RichText: richTextChunks(strings.TrimSpace(strings.TrimPrefix(trimmed, "- [ ] "))), Checked: &checked}, true
	case strings.HasPrefix(strings.ToLower(trimmed), "- [x] "):
		checked := true
		return &notion.ToDoBlock{RichText: richTextChunks(strings.TrimSpace(trimmed[6:])), Checked: &checked}, true
	case strings.HasPrefix(trimmed, "- "):
		return &notion.BulletedListItemBlock{RichText: richTextChunks(strings.TrimSpace(strings.TrimPrefix(trimmed, "- ")))}, true
	case numberedListRegex.MatchString(trimmed):
		return &notion.NumberedListItemBlock{RichText: richTextChunks(numberedListRegex.ReplaceAllString(trimmed, ""))}, true
	case strings.HasPrefix(trimmed, "> "):
		return &notion.QuoteBlock{RichText: richTextChunks(strings.TrimSpace(strings.TrimPrefix(trimmed, "> ")))}, true
	}
	return nil, false
}

var numberedListRegex = regexp.MustCompile(`^\d+\.\s+`)

func richTextChunks(text string) []notion.RichText {
	if text == "" {
		return nil
	}
	const maxRichTextLen = 1900
	var chunks []notion.RichText
	runes := []rune(text)
	for len(runes) > 0 {
		n := maxRichTextLen
		if len(runes) < n {
			n = len(runes)
		}
		chunk := string(runes[:n])
		runes = runes[n:]
		chunks = append(chunks, notion.RichText{
			Type:      notion.RichTextTypeText,
			PlainText: chunk,
			Text:      &notion.Text{Content: chunk},
		})
	}
	return chunks
}
