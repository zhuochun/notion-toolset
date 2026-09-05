package notionops

import (
	"bytes"
	"fmt"
	"html/template"
)

type PageBuilder struct {
	Title      string
	Date       string
	DateEnd    string
	DatabaseID string
}

func Tmpl(name, s string, builder interface{}) ([]byte, error) {
	tmpl, err := template.New(name).Parse(s)
	if err != nil {
		return nil, fmt.Errorf("template %s parse: %w", name, err)
	}

	var raw bytes.Buffer
	if err = tmpl.Execute(&raw, builder); err != nil {
		return nil, fmt.Errorf("template %s execute: %w", name, err)
	}

	return raw.Bytes(), nil
}

type BlockBuilder struct {
	Date    string
	Content string
	PageID  string
}
