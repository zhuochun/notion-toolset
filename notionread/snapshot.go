package notionread

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/dstotijn/go-notion"
)

type CompletenessMode int

const (
	// Strict rejects the entire snapshot when any nested read fails.
	Strict CompletenessMode = iota
	// BestEffort records nested failures while retaining unrelated branches.
	BestEffort
)

type Issue struct {
	BlockID string
	Err     error
}

type BlockSnapshot struct {
	roots               []notion.Block
	children            map[string][]notion.Block
	errors              map[string]error
	issues              []Issue
	loaded              []string
	crossPageBoundaries bool
}

// NewSnapshot creates a read-only snapshot value from already loaded blocks.
func NewSnapshot(rootID string, roots []notion.Block, children map[string][]notion.Block) BlockSnapshot {
	snapshot := BlockSnapshot{
		roots:    cloneBlocks(roots),
		children: cloneChildren(children),
		errors:   map[string]error{},
	}
	if rootID != "" {
		snapshot.children[rootID] = cloneBlocks(roots)
		snapshot.loaded = append(snapshot.loaded, rootID)
	}
	ids := make([]string, 0, len(children))
	for id := range children {
		if id != rootID {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	snapshot.loaded = append(snapshot.loaded, ids...)
	return snapshot
}

func (r *Reader) BlockSnapshot(ctx context.Context, rootID string, mode CompletenessMode) (BlockSnapshot, error) {
	snapshot, err := r.blockForest(ctx, []string{rootID}, mode, false)
	if err != nil {
		return BlockSnapshot{}, err
	}
	snapshot.roots = cloneBlocks(snapshot.children[rootID])
	return snapshot, nil
}

// BlockForest builds one snapshot across multiple roots so shared descendants
// are fetched once for the complete operation.
func (r *Reader) BlockForest(ctx context.Context, rootIDs []string, mode CompletenessMode) (BlockSnapshot, error) {
	return r.blockForest(ctx, rootIDs, mode, true)
}

func (r *Reader) blockForest(ctx context.Context, rootIDs []string, mode CompletenessMode, crossPageBoundaries bool) (BlockSnapshot, error) {
	snapshot := NewSnapshot("", nil, map[string][]notion.Block{})
	snapshot.crossPageBoundaries = crossPageBoundaries
	visited := make(map[string]struct{}, len(rootIDs))
	queue := []notion.Block{}

	for _, rootID := range rootIDs {
		if _, ok := visited[rootID]; ok {
			continue
		}
		visited[rootID] = struct{}{}
		roots, err := r.BlockChildren(ctx, rootID)
		if err != nil {
			return BlockSnapshot{}, err
		}
		snapshot.children[rootID] = cloneBlocks(roots)
		snapshot.loaded = append(snapshot.loaded, rootID)
		queue = append(queue, roots...)
	}

	for len(queue) > 0 {
		childIDs := []string{}
		for _, block := range queue {
			childID := snapshot.childSourceID(block)
			if childID == "" {
				continue
			}
			if _, ok := visited[childID]; ok {
				continue
			}
			visited[childID] = struct{}{}
			childIDs = append(childIDs, childID)
		}
		queue = nil
		if len(childIDs) == 0 {
			continue
		}

		results := r.readBlockChildren(ctx, childIDs)
		for i, result := range results {
			childID := childIDs[i]
			if result.err != nil {
				if errors.Is(result.err, context.Canceled) || errors.Is(result.err, context.DeadlineExceeded) {
					return BlockSnapshot{}, result.err
				}
				if mode == Strict {
					return BlockSnapshot{}, fmt.Errorf("build Notion block snapshot: %w", result.err)
				}
				snapshot.children[childID] = cloneBlocks(result.blocks)
				snapshot.errors[childID] = result.err
				snapshot.issues = append(snapshot.issues, Issue{BlockID: childID, Err: result.err})
				queue = append(queue, result.blocks...)
				continue
			}
			snapshot.children[childID] = cloneBlocks(result.blocks)
			snapshot.loaded = append(snapshot.loaded, childID)
			queue = append(queue, result.blocks...)
		}
	}

	return snapshot, nil
}

type blockChildrenResult struct {
	blocks []notion.Block
	err    error
}

func (r *Reader) readBlockChildren(ctx context.Context, blockIDs []string) []blockChildrenResult {
	results := make([]blockChildrenResult, len(blockIDs))
	jobs := make(chan int)
	workerCount := min(r.concurrency, len(blockIDs))
	var wg sync.WaitGroup

	for range workerCount {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for index := range jobs {
				results[index].blocks, results[index].err = r.BlockChildren(ctx, blockIDs[index])
			}
		}()
	}
	for index := range blockIDs {
		jobs <- index
	}
	close(jobs)
	wg.Wait()
	return results
}

func (s BlockSnapshot) Roots() []notion.Block {
	return cloneBlocks(s.roots)
}

func (s BlockSnapshot) Children(blockID string) ([]notion.Block, error) {
	blocks, hasBlocks := s.children[blockID]
	err, hasError := s.errors[blockID]
	if !hasBlocks && !hasError {
		return nil, fmt.Errorf("block children %s are not present in snapshot", blockID)
	}
	return cloneBlocks(blocks), err
}

func (s BlockSnapshot) Issues() []Issue {
	return append([]Issue(nil), s.issues...)
}

func (s BlockSnapshot) LoadedBlockIDs() []string {
	return append([]string(nil), s.loaded...)
}

func (s BlockSnapshot) childSourceID(block notion.Block) string {
	if !s.crossPageBoundaries {
		switch block.(type) {
		case *notion.ChildPageBlock, *notion.ChildDatabaseBlock:
			return ""
		}
	}
	if synced, ok := block.(*notion.SyncedBlock); ok && synced.SyncedFrom != nil {
		return synced.SyncedFrom.BlockID
	}
	if block.HasChildren() {
		return block.ID()
	}
	return ""
}

func cloneBlocks(blocks []notion.Block) []notion.Block {
	return append([]notion.Block(nil), blocks...)
}

func cloneChildren(children map[string][]notion.Block) map[string][]notion.Block {
	cloned := make(map[string][]notion.Block, len(children))
	for id, blocks := range children {
		cloned[id] = cloneBlocks(blocks)
	}
	return cloned
}
