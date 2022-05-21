package transformer

import (
	"net/url"
	"strings"
)

// AssetFuture ...
type AssetFuture struct {
	BlockID   string
	URL       string
	Extension string

	done     chan struct{}
	err      error
	filename string
}

func NewAssetFuture(id, imgUrl string) *AssetFuture {
	f := &AssetFuture{
		BlockID: id,
		URL:     imgUrl,

		done: make(chan struct{}),
	}

	u, err := url.Parse(imgUrl)
	if err != nil {
		f.Extension = ""
	}

	idx := strings.LastIndex(u.Path, ".")
	if idx != -1 {
		f.Extension = strings.ToLower(u.Path[idx:])
	}

	return f
}

func (f *AssetFuture) Write(filename string, err error) {
	f.filename = filename
	f.err = err

	close(f.done)
}

func (f *AssetFuture) Read() (string, error) {
	<-f.done
	return f.filename, f.err
}
