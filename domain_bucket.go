package adblockr

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
)

const globChars = "*?[]"

type DomainBucket interface {
	Put(key string, value bool) error
	Has(domain string) bool
	Forget(key string)
	Update(uri string) (int, error)
}

func getDomainResource(uri string) (io.ReadCloser, error) {
	srcUrl, err := url.Parse(uri)
	if err != nil {
		return nil, err
	}

	if srcUrl.Scheme == "file" {
		file, err := os.Open(srcUrl.Host + srcUrl.Path)
		if err != nil {
			return nil, err
		}
		return file, nil
	}

	resp, err := http.Get(uri)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("HTTP %d %s", resp.StatusCode, resp.Status)
	}

	contentType := resp.Header.Get("Content-Type")
	if !strings.Contains(contentType, "text/plain") {
		resp.Body.Close()
		return nil, fmt.Errorf("invalid content type: %s", contentType)
	}

	return resp.Body, nil
}
