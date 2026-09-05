package exporter

import (
	"context"
	"fmt"
	"io"
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

func (e *Exporter) StartDownloader(wg *sync.WaitGroup, size int) chan *transformer.AssetFuture {
	taskPool := make(chan *transformer.AssetFuture, size)

	for i := 0; i < size; i++ {
		wg.Add(1)

		go func() {
			for asset := range taskPool {
				filename, err := e.downloadAsset(asset)
				asset.Write(filename, err)

				if err != nil {
					e.logger().Printf("Failed to download: %v", err)
				}
			}

			wg.Done()
		}()
	}

	return taskPool
}

var assetExtension = regexp.MustCompile(`(?i)\.(png|jpe?g|gif|webp|mp4|mov|webm|mkv|avi|mp3|wav|m4a|flac|ogg|pdf)$`)

func (e *Exporter) downloadAsset(asset *transformer.AssetFuture) (string, error) {
	if e.AssetDirectory == "" {
		return "", fmt.Errorf("config assetDirectory is empty")
	}

	if !assetExtension.MatchString(asset.Extension) {
		return "", fmt.Errorf("unsupported extension: %v", asset.Extension)
	}

	filename := e.getAssetFilename(asset)

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

	client := e.AssetClient
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

func (e *Exporter) getAssetFilename(asset *transformer.AssetFuture) string {
	return filepath.Join(e.AssetDirectory, transformer.SimpleID(asset.BlockID)+asset.Extension)
}
