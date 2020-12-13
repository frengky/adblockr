package adblockr

import (
	"bufio"
	"github.com/gobwas/glob"
	"strings"
	"sync"
)

func NewMemDomainBucket() DomainBucket {
	return &MemDomainBucket{
		domains:  make(map[string]bool),
		patterns: make(map[string]glob.Glob),
		mu:       sync.RWMutex{},
	}
}

type MemDomainBucket struct {
	domains  map[string]bool
	patterns map[string]glob.Glob
	mu       sync.RWMutex
}

func (m *MemDomainBucket) Put(key string, value bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if strings.ContainsAny(key, globChars) {
		g, err := glob.Compile(key)
		if err != nil {
			return err
		}
		m.patterns[key] = g
	} else {
		m.domains[strings.ToLower(key)] = value
	}
	return nil
}

func (m *MemDomainBucket) putNoLock(key string, value bool) error {
	if strings.ContainsAny(key, globChars) {
		g, err := glob.Compile(key)
		if err != nil {
			return err
		}
		m.patterns[key] = g
	} else {
		m.domains[strings.ToLower(key)] = value
	}
	return nil
}

func (m *MemDomainBucket) Has(domain string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	domain = strings.ToLower(domain)
	_, ok := m.domains[domain]
	if ok {
		return true
	}

	for _, g := range m.patterns {
		if g.Match(domain) {
			return true
		}
	}
	return false
}

func (m *MemDomainBucket) Forget(key string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if strings.ContainsAny(key, globChars) {
		delete(m.patterns, key)
	} else {
		delete(m.domains, strings.ToLower(key))
	}
}

func (m *MemDomainBucket) Update(uri string) (int, error) {
	r, err := getDomainResource(uri)
	if err != nil {
		return 0, err
	}
	defer r.Close()

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
			if err := m.putNoLock(line, true); err == nil {
				count++
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return 0, err
	}

	return count, nil
}
