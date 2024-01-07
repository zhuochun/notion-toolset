package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html/template"

	"github.com/dstotijn/go-notion"
)

type QueryBuilder struct {
	Date string
}

type PageBuilder struct {
	Title      string
	Date       string
	DateEnd    string
	DatabaseID string
}

type BlockBuilder struct {
	Date   string
	PageID string
}

type ToggleBuilder struct {
	Title string
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

type AppendBlock struct {
	Client         *notion.Client
	AppendToPageID string

	Block []byte
}

func NewAppendBlock(c *notion.Client, appendTo string) *AppendBlock {
	return &AppendBlock{
		Client:         c,
		AppendToPageID: appendTo,
	}
}

func (a *AppendBlock) SetBlock(name, s string, builder interface{}) error {
	if s == "" {
		return fmt.Errorf("empty block text: %s", name)
	}

	block, err := Tmpl(name, s, builder)
	if err != nil {
		return err
	}
	a.Block = block
	return nil
}

// https://pkg.go.dev/github.com/dstotijn/go-notion#ParagraphBlock
// https://pkg.go.dev/github.com/dstotijn/go-notion#RichText
func (a *AppendBlock) WriteParagraph(ctx context.Context) (notion.BlockChildrenResponse, error) {
	block := &notion.ParagraphBlock{}
	if err := json.Unmarshal(a.Block, block); err != nil {
		return notion.BlockChildrenResponse{}, fmt.Errorf("unmarshal Block: %w", err)
	}

	return a.Client.AppendBlockChildren(ctx, a.AppendToPageID, []notion.Block{block})
}
