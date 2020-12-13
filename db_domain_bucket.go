package adblockr

import (
	"bufio"
	"fmt"
	"github.com/gobwas/glob"
	"github.com/joyrexus/buckets"
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

func (s *DbDomainBucket) Update(uri string) (int, error) {
	r, err := getDomainResource(uri)
	if err != nil {
		return 0, err
	}

	defaultValue := []byte(strconv.FormatBool(true))
	var domains, patterns []struct {
		Key, Value []byte
	}

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
			if strings.ContainsAny(line, globChars) {
				g, err := glob.Compile(line)
				if err == nil {
					s.patterns[line] = g
					patterns = append(patterns, struct {
						Key, Value []byte
					}{
						[]byte(line), defaultValue,
					})
					count++
				}
			} else {
				domains = append(domains, struct {
					Key, Value []byte
				}{
					[]byte(strings.ToLower(line)), defaultValue,
				})
				count++
			}
		}
	}
	r.Close()

	if err := scanner.Err(); err != nil {
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
