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

type DbDomainStore struct {
	db      *buckets.DB
	bucket  *buckets.Bucket
	special map[string]glob.Glob
	mu      sync.RWMutex
}

func NewDbDomainStore() DomainStore {
	return &DbDomainStore{
		special: make(map[string]glob.Glob),
		mu:      sync.RWMutex{},
	}
}

func (s *DbDomainStore) Open(filepath string) error {
	var err error
	s.db, err = buckets.Open(filepath)
	if err != nil {
		return err
	}
	s.bucket, err = s.db.New([]byte("domains"))
	return err
}

func (s *DbDomainStore) Close() error {
	return s.db.Close()
}

func (s *DbDomainStore) Put(key string, value bool) error {
	key = strings.ToLower(key)
	if strings.ContainsAny(key, globChars) {
		g, err := glob.Compile(key)
		if err == nil {
			s.mu.Lock()
			s.special[key] = g
			s.mu.Unlock()
			return nil
		}
		return fmt.Errorf("invalid special entry: `%s` %v", key, err)
	}
	return s.bucket.Put([]byte(key), []byte(strconv.FormatBool(value)))
}

func (s *DbDomainStore) Has(domain string) bool {
	domain = strings.ToLower(domain)
	val, err := s.bucket.Get([]byte(domain))
	if err == nil && len(val) >= 4 {
		return true
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, g := range s.special {
		if g.Match(domain) {
			return true
		}
	}
	return false
}

func (s *DbDomainStore) Forget(key string) {
	key = strings.ToLower(key)
	if strings.ContainsAny(key, globChars) {
		s.mu.Lock()
		delete(s.special, key)
		s.mu.Unlock()
	} else {
		s.bucket.Delete([]byte(key))
	}
}

func (s *DbDomainStore) Update(uri string) (int, error) {
	r, err := getDomainResource(uri)
	if err != nil {
		return 0, err
	}

	defaultValue := []byte(strconv.FormatBool(true))
	var domains []struct {
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
			domains = append(domains, struct {
				Key, Value []byte
			}{
				[]byte(line),
				defaultValue,
			})
			count++
		}
	}
	r.Close()

	if err := scanner.Err(); err != nil {
		return 0, nil
	}
	return count, s.bucket.Insert(domains)
}
