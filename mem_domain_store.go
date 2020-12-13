package adblockr

import (
	"bufio"
	"github.com/gobwas/glob"
	"strings"
	"sync"
)

func NewMemDomainStore() DomainStore {
	return &MemDomainStore{
		backend: make(map[string]bool),
		special: make(map[string]glob.Glob),
		mu:      sync.RWMutex{},
	}
}

type MemDomainStore struct {
	backend map[string]bool
	special map[string]glob.Glob
	mu      sync.RWMutex
}

func (m *MemDomainStore) Put(key string, value bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	key = strings.ToLower(key)
	if strings.ContainsAny(key, globChars) {
		g, err := glob.Compile(key)
		if err != nil {
			return err
		}
		m.special[key] = g
	} else {
		m.backend[key] = value
	}
	return nil
}

func (m *MemDomainStore) putNoLock(key string, value bool) error {
	key = strings.ToLower(key)
	if strings.ContainsAny(key, globChars) {
		g, err := glob.Compile(key)
		if err != nil {
			return err
		}
		m.special[key] = g
	} else {
		m.backend[key] = value
	}
	return nil
}

func (m *MemDomainStore) Has(domain string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	domain = strings.ToLower(domain)
	_, ok := m.backend[domain]
	if ok {
		return true
	}

	for _, g := range m.special {
		if g.Match(domain) {
			return true
		}
	}
	return false
}

func (m *MemDomainStore) Forget(key string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	key = strings.ToLower(key)
	if strings.ContainsAny(key, globChars) {
		delete(m.special, key)
	} else {
		delete(m.backend, key)
	}
}

func (m *MemDomainStore) Update(uri string) (int, error) {
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
		return 0, nil
	}

	return count, nil
}
