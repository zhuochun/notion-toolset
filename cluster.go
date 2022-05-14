package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"unicode/utf8"

	"github.com/dstotijn/go-notion"
	"github.com/go-ego/gse"
	"github.com/zhuochun/notion-toolset/transformer"
)

type ClusterConfig struct {
	DatabaseID             string `yaml:"databaseID"`
	DatabaseQuery          string `yaml:"databaseQuery"`
	MinClusterNodes        int    `yaml:"minClusterNodes"` // optional, default=3
	ClusterNums            int    `yaml:"clusterNums"`     // optional
	CollectDumpID          string `yaml:"collectDumpID"`
	CollectDumpBlockToggle string `yaml:"collectDumpBlockToggle"`
	CollectDumpBlock       string `yaml:"collectDumpBlock"`
}

type clusterTable struct {
	key   string
	pages []notion.Page
}

type clusterList []*clusterTable

func (a clusterList) Len() int           { return len(a) }
func (a clusterList) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a clusterList) Less(i, j int) bool { return len(a[i].pages) > len(a[j].pages) }

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
				// remove 1 character word
				if utf8.RuneCount([]byte(word)) <= 1 {
					continue
				}
				// remove duplicated words in title
				if _, ok := dedup[word]; ok {
					continue
				} else {
					dedup[word] = struct{}{}
				}

				if v, ok := clusters[word]; ok {
					v.pages = append(v.pages, page)
				} else {
					clusters[word] = &clusterTable{
						key:   word,
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

	if len(clusters) == 0 {
		return nil
	}

	if c.MinClusterNodes == 0 {
		c.MinClusterNodes = 3
	}

	topClusters := c.TopClusters(clusters)

	if _, err := c.WriteClusters(topClusters); err != nil {
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

func (c *Cluster) TopClusters(clusters map[string]*clusterTable) []*clusterTable {
	totalPages := 0
	sortedClusters := make(clusterList, 0, len(clusters)) // TODO max heap

	for _, cluster := range clusters {
		if len(cluster.pages) < c.MinClusterNodes {
			continue // skip mini clusters
		}

		totalPages += len(cluster.pages)
		sortedClusters = append(sortedClusters, cluster)
	}

	sort.Sort(sortedClusters)

	clusterNums := c.ClusterNums
	// make an assumption based on 80/20 rule, the top 20% of the clusters are actual clusters
	estClusterNums := len(sortedClusters) / 5
	// set the defaults
	if clusterNums == 0 {
		clusterNums = estClusterNums
	}
	// make sure the cluster nums are reasonable
	if len(sortedClusters) < 10 || clusterNums > len(sortedClusters) {
		clusterNums = len(sortedClusters)
	}

	// TODO fine tune the cluster nums?
	avgPages := totalPages / len(sortedClusters)

	log.Printf("Num of clusters: %v. Est clusters: %v, Avg clusters nodes: %v", clusterNums, estClusterNums, avgPages)
	return sortedClusters[0:clusterNums]
}

func (c *Cluster) WriteClusters(clusters []*clusterTable) (notion.BlockChildrenResponse, error) {
	for _, cluster := range clusters {
		if c.DebugMode {
			log.Printf("Write cluster: %v, pages: %v", cluster.key, len(cluster.pages))
		}

		// create Toggle
		toggleData, err := Tmpl("ClusterBlockToggle", c.CollectDumpBlockToggle, ToggleBuilder{
			Title: fmt.Sprintf("%v (%v)", cluster.key, len(cluster.pages)),
		})
		if err != nil {
			return notion.BlockChildrenResponse{}, err
		}

		toggleBlock := notion.Block{}
		if err := json.Unmarshal(toggleData, &toggleBlock); err != nil {
			return notion.BlockChildrenResponse{}, fmt.Errorf("unmarshal Block: %w", err)
		}

		// create Children
		for i, page := range cluster.pages {
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

			if i == 99 { // maximum 100 children in a request
				break
			}
		}

		if resp, err := c.Client.AppendBlockChildren(context.TODO(), c.CollectDumpID, []notion.Block{toggleBlock}); err != nil {
			return resp, err
		}
	}
	return notion.BlockChildrenResponse{}, nil
}
