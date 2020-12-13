package adblockr

import (
	"bufio"
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

func OpenResource(uri string) (io.ReadCloser, error) {
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

func ParseLine(r io.Reader, handler func(line string) bool) (int, error) {
	count := 0
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		line = strings.Split(line, "#")[0]
		line = strings.TrimSpace(line)

		if len(line) > 0 {
			fields := strings.Fields(line)
			if len(fields) > 1 {
				line = fields[1]
			} else {
				line = fields[0]
			}
			if handler(line) {
				count++
			}
		}
	}

	return count, scanner.Err()
}
