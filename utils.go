package main

import (
	"bytes"
	"fmt"
	"html/template"
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
