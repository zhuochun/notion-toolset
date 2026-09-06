package exporter

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"

	"github.com/zhuochun/notion-toolset/transformer"
)

var exportAssetHTTPClient = &http.Client{
	Timeout: 30 * time.Second,
}

// assetDownloader needs no database, traversal, or export-cleanup state.
type assetDownloader struct {
	directory string
	client    *http.Client
	logger    *log.Logger
}

func (d assetDownloader) start(wg *sync.WaitGroup, size int) chan *transformer.AssetFuture {
	logger := d.logger
	if logger == nil {
		logger = log.Default()
	}
	taskPool := make(chan *transformer.AssetFuture, size)

	for i := 0; i < size; i++ {
		wg.Add(1)

		go func() {
			for asset := range taskPool {
				filename, err := d.download(asset)
				asset.Write(filename, err)

				if err != nil {
					logger.Printf("Failed to download: %v", err)
				}
			}

			wg.Done()
		}()
	}

	return taskPool
}

var assetExtension = regexp.MustCompile(`(?i)\.(png|jpe?g|gif|webp|mp4|mov|webm|mkv|avi|mp3|wav|m4a|flac|ogg|pdf)$`)

func (d assetDownloader) download(asset *transformer.AssetFuture) (string, error) {
	if d.directory == "" {
		return "", fmt.Errorf("config assetDirectory is empty")
	}

	if !assetExtension.MatchString(asset.Extension) {
		return "", fmt.Errorf("unsupported extension: %v", asset.Extension)
	}

	filename := filepath.Join(d.directory, transformer.SimpleID(asset.BlockID)+asset.Extension)

	if _, err := os.Stat(filename); err == nil {
		return filename, nil
	}

	file, err := os.Create(filename)
	if err != nil {
		return filename, fmt.Errorf("create file, name: %v, err: %v", filename, err)
	}
	defer file.Close()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, asset.URL, nil)
	if err != nil {
		return "", err
	}

	client := d.client
	if client == nil {
		client = exportAssetHTTPClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("statusCode: %v, URL: %v", resp.StatusCode, asset.URL)
	}

	if _, err := io.Copy(file, resp.Body); err != nil {
		return filename, fmt.Errorf("write file, URL: %v, err: %v", asset.URL, err)
	}

	return filename, nil
}
