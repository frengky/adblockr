package adblockr

import (
	"bytes"
	"fmt"
	"github.com/miekg/dns"
	log "github.com/sirupsen/logrus"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"strings"
)

const dnsContentType = "application/dns-message"

func NewDohResolver(dohUrl string, client *http.Client) Resolver {
	host := dohUrl
	if url, err := url.Parse(dohUrl); err != nil {
		host = url.Host
	}
	return &dohResolver{
		host:   host,
		url:    dohUrl,
		client: client,
	}
}

type dohResolver struct {
	host   string
	url    string
	client *http.Client
}

func (d *dohResolver) Lookup(net string, req *dns.Msg) (*dns.Msg, error) {
	logCtx := log.WithFields(log.Fields{
		"upstream": d.host,
		"qname":    req.Question[0].Name,
		"network":  net + "->https",
	})

	data, err := req.Pack()
	if err != nil {
		return nil, err
	}

	reader := bytes.NewReader(data)
	resp, err := d.client.Post(d.url, dnsContentType, reader)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		io.Copy(ioutil.Discard, resp.Body)
		logCtx.WithField("status", resp.StatusCode).Error("invalid response status code from upstream")
		return nil, fmt.Errorf("error while resolving from upstream")
	}

	contentType := resp.Header.Get("Content-Type")
	if !strings.Contains(contentType, dnsContentType) {
		io.Copy(ioutil.Discard, resp.Body)
		logCtx.WithField("content-type", contentType).Error("invalid response format from upstream")
		return nil, fmt.Errorf("error while resolving from upstream")
	}

	respPacket, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	res := dns.Msg{}
	err = res.Unpack(respPacket)
	if err != nil {
		return nil, err
	}

	logCtx.Debug("resolving with DoH")
	return &res, nil
}
