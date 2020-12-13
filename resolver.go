package adblockr

import (
	"fmt"
	"github.com/miekg/dns"
	log "github.com/sirupsen/logrus"
	"sync"
	"time"
)

type Resolver interface {
	Lookup(net string, req *dns.Msg) (*dns.Msg, error)
}

type defaultResolver struct {
	nameservers []string
	interval    int
	timeout     int
}

func NewResolver(nameservers []string, intervalMs int, timeoutSecs int) Resolver {
	return &defaultResolver{
		nameservers: nameservers,
		interval:    intervalMs,
		timeout:     timeoutSecs,
	}
}

func (r *defaultResolver) Lookup(net string, req *dns.Msg) (*dns.Msg, error) {

	c := &dns.Client{
		Net:          net,
		ReadTimeout:  time.Duration(r.timeout) * time.Second,
		WriteTimeout: time.Duration(r.timeout) * time.Second,
	}

	qName := req.Question[0].Name

	res := make(chan *dns.Msg, 1)
	var wg sync.WaitGroup
	L := func(nameserver string) {
		defer wg.Done()
		rr, _, err := c.Exchange(req, nameserver)
		if err != nil {
			log.WithField("ns", nameserver).WithError(err).Error("error while resolving from upstream")
			return
		}
		if rr != nil && rr.Rcode != dns.RcodeSuccess {
			log.WithField("ns", nameserver).WithError(err).Warn("invalid answer from upstream")
			if rr.Rcode == dns.RcodeServerFailure {
				return
			}
		} else {
			log.WithFields(log.Fields{
				"upstream": nameserver,
				"qname":    qName,
				"network":  net,
			}).Debug("resolving with upstream")
		}
		select {
		case res <- rr:
		default:
		}
	}

	ticker := time.NewTicker(time.Duration(r.interval) * time.Millisecond)
	defer ticker.Stop()

	for _, ns := range r.nameservers {
		wg.Add(1)
		go L(ns)
		select {
		case msg := <-res:
			return msg, nil
		case <-ticker.C:
			continue
		}
	}

	wg.Wait()
	select {
	case msg := <-res:
		return msg, nil
	default:
		return nil, fmt.Errorf("error while resolving from upstream")
	}
}
