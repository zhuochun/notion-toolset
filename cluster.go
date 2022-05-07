package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"unicode/utf8"

	"github.com/dstotijn/go-notion"
	"github.com/go-ego/gse"
	"github.com/zhuochun/notion-toolset/transformer"
)

type ClusterConfig struct {
	DatabaseID             string `yaml:"databaseID"`
	DatabaseQuery          string `yaml:"databaseQuery"`
	CollectDumpID          string `yaml:"collectDumpID"`
	CollectDumpBlockToggle string `yaml:"collectDumpBlockToggle"`
	CollectDumpBlock       string `yaml:"collectDumpBlock"`
}

type clusterTable struct {
	pages []notion.Page
}

type Cluster struct {
	DebugMode bool

	Client *notion.Client
	ClusterConfig
}

var seg gse.Segmenter

// TODO provide update and refresh the toggles
func (c *Cluster) Run() error {
	seg.LoadDict()
	seg.LoadStop("zh, data/stop_words.txt")

	pagesChan, errChan := c.ScanPages()
	pageNum := 0
	clusters := map[string]*clusterTable{}
	for pages := range pagesChan {
		for _, page := range pages {
			pageNum += 1

			title, err := transformer.GetPageTitle(page)
			if err != nil {
				log.Printf("Err pageID: %v, err: %v", page.ID, err)
				continue
			}

			words := seg.PosTrimArr(title, true, "v", "u", "d", "m", "r")

			dedup := map[string]struct{}{}
			for _, word := range words {
				if _, ok := dedup[word]; ok {
					continue
				} else {
					dedup[word] = struct{}{}
				}

				if v, ok := clusters[word]; ok {
					v.pages = append(v.pages, page)
				} else {
					clusters[word] = &clusterTable{
						pages: []notion.Page{page},
					}
				}
			}

			if c.DebugMode && pageNum%500 == 0 {
				log.Printf("Scanned pages: %v so far", pageNum)
			}
		}
	}
	log.Printf("Scanned pages: %v, clusters: %v", pageNum, len(clusters))

	select {
	case err := <-errChan:
		return err
	default:
	}

	if _, err := c.WriteClusters(pageNum, clusters); err != nil {
		log.Printf("Write errored. err: %v", err)
	}
	return nil
}

func (c *Cluster) ScanPages() (chan []notion.Page, chan error) {
	q := NewDatabaseQuery(c.Client, c.DatabaseID)

	if err := q.SetQuery(c.DatabaseQuery, QueryBuilder{}); err != nil {
		log.Panicf("Invalid query: %v, err: %v", c.DatabaseQuery, err)
	}

	if c.DebugMode {
		log.Printf("DatabaseQuery Filter: %+v", q.Query.Filter)
		log.Printf("DatabaseQuery Sorter: %+v", q.Query.Sorts)
	}

	return q.Go(context.TODO(), 3)
}

func (c *Cluster) WriteClusters(total int, clusters map[string]*clusterTable) (notion.BlockChildrenResponse, error) {
	lower_bound := 20
	upper_bound := 90
	log.Printf("Filter cluster: [%v, %v]", lower_bound, upper_bound)

	for w, cluster := range clusters {
		if utf8.RuneCount([]byte(w)) < 1 {
			continue
		}

		if len(cluster.pages) < lower_bound || len(cluster.pages) > upper_bound {
			continue
		}

		if c.DebugMode {
			log.Printf("Write cluster: %v, pages: %v", w, len(cluster.pages))
		}

		// create Toggle
		toggleData, err := Tmpl("ClusterBlockToggle", c.CollectDumpBlockToggle, ToggleBuilder{
			Title: w,
		})
		if err != nil {
			return notion.BlockChildrenResponse{}, err
		}

		toggleBlock := notion.Block{}
		if err := json.Unmarshal(toggleData, &toggleBlock); err != nil {
			return notion.BlockChildrenResponse{}, fmt.Errorf("unmarshal Block: %w", err)
		}

		// create Children
		for _, page := range cluster.pages {
			blockData, err := Tmpl("ClusterBlock", c.CollectDumpBlock, BlockBuilder{
				PageID: page.ID,
			})
			if err != nil {
				return notion.BlockChildrenResponse{}, err
			}

			block := notion.Block{}
			if err := json.Unmarshal(blockData, &block); err != nil {
				return notion.BlockChildrenResponse{}, fmt.Errorf("unmarshal Block: %w", err)
			}

			toggleBlock.Toggle.Children = append(toggleBlock.Toggle.Children, block)
		}

		if resp, err := c.Client.AppendBlockChildren(context.TODO(), c.CollectDumpID, []notion.Block{toggleBlock}); err != nil {
			return resp, err
		}
	}
	return notion.BlockChildrenResponse{}, nil
}
