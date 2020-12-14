package adblockr

import (
	"github.com/gobwas/glob"
	"io"
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

func (m *MemDomainBucket) Update(list io.Reader) (int, error) {
	count, err := ParseLine(list, func(line string) bool {
		if err := m.putNoLock(line, true); err == nil {
			return true
		}
		return false
	})

	if err != nil {
		return 0, err
	}

	return count, nil
}
