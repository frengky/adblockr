package adblockr

import (
	"fmt"
	"github.com/gobwas/glob"
	"github.com/joyrexus/buckets"
	"io"
	"strconv"
	"strings"
	"sync"
)

const (
	domainBucket  = "domains"
	patternBucket = "patterns"
)

type DbDomainBucket struct {
	db       *buckets.DB
	dBucket  *buckets.Bucket
	pBucket  *buckets.Bucket
	patterns map[string]glob.Glob
	mu       sync.RWMutex
}

func NewDbDomainBucket() DomainBucket {
	return &DbDomainBucket{
		patterns: make(map[string]glob.Glob),
		mu:       sync.RWMutex{},
	}
}

func (s *DbDomainBucket) Open(filepath string) error {
	var err error
	s.db, err = buckets.Open(filepath)
	if err != nil {
		return err
	}
	s.dBucket, err = s.db.New([]byte(domainBucket))
	if err != nil {
		return err
	}
	s.pBucket, err = s.db.New([]byte(patternBucket))
	if err != nil {
		return err
	}
	if items, err := s.pBucket.Items(); err == nil {
		for _, p := range items {
			key := string(p.Key)
			if g, err := glob.Compile(key); err == nil {
				s.patterns[key] = g
			}
		}
	}
	return nil
}

func (s *DbDomainBucket) Close() error {
	return s.db.Close()
}

func (s *DbDomainBucket) Put(key string, value bool) error {
	if strings.ContainsAny(key, globChars) {
		g, err := glob.Compile(key)
		if err == nil {
			s.mu.Lock()
			s.patterns[key] = g
			s.mu.Unlock()
			return s.pBucket.Put([]byte(key), []byte(strconv.FormatBool(value)))
		}
		return fmt.Errorf("invalid patterns entry: `%s` %v", key, err)
	}
	return s.dBucket.Put([]byte(strings.ToLower(key)), []byte(strconv.FormatBool(value)))
}

func (s *DbDomainBucket) Has(domain string) bool {
	domain = strings.ToLower(domain)
	val, err := s.dBucket.Get([]byte(domain))
	if err == nil && len(val) >= 4 {
		return true
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, g := range s.patterns {
		if g.Match(domain) {
			return true
		}
	}
	return false
}

func (s *DbDomainBucket) Forget(key string) {
	if strings.ContainsAny(key, globChars) {
		s.mu.Lock()
		delete(s.patterns, key)
		s.mu.Unlock()
		s.pBucket.Delete([]byte(key))
	} else {
		s.dBucket.Delete([]byte(strings.ToLower(key)))
	}
}

func (s *DbDomainBucket) Update(list io.Reader) (int, error) {
	defaultValue := []byte(strconv.FormatBool(true))
	var domains, patterns []struct {
		Key, Value []byte
	}

	count, err := ParseLine(list, func(line string) bool {
		if strings.ContainsAny(line, globChars) {
			g, err := glob.Compile(line)
			if err == nil {
				s.patterns[line] = g
				patterns = append(patterns, struct {
					Key, Value []byte
				}{
					[]byte(line), defaultValue,
				})
				return true
			}
		} else {
			domains = append(domains, struct {
				Key, Value []byte
			}{
				[]byte(strings.ToLower(line)), defaultValue,
			})
			return true
		}
		return false
	})

	if err != nil {
		return 0, err
	}

	if len(domains) > 0 {
		s.dBucket.Insert(domains)
	}
	if len(patterns) > 0 {
		s.pBucket.Insert(patterns)
	}

	return count, nil
}
