package server

import (
	"strings"
	"sync"
)

const (
	localAudioPrefix = "local-audio:"
	localImagePrefix = "local-image:"
)

type localMediaEntry struct {
	Data     []byte
	Filename string
	Format   string
}

type localMediaStore struct {
	mu   sync.Mutex
	byID map[string]localMediaEntry
}

func newLocalMediaStore() *localMediaStore {
	return &localMediaStore{byID: make(map[string]localMediaEntry)}
}

func (s *localMediaStore) put(uri string, data []byte, filename, format string) {
	if uri == "" || len(data) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.byID[uri] = localMediaEntry{Data: data, Filename: filename, Format: format}
}

func (s *localMediaStore) take(uri string) (localMediaEntry, bool) {
	if uri == "" {
		return localMediaEntry{}, false
	}
	key := uri
	if strings.HasPrefix(uri, localAudioPrefix) {
		key = strings.TrimPrefix(uri, localAudioPrefix)
	} else if strings.HasPrefix(uri, localImagePrefix) {
		key = strings.TrimPrefix(uri, localImagePrefix)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.byID[key]
	if !ok {
		entry, ok = s.byID[uri]
	}
	if ok {
		delete(s.byID, key)
		if key != uri {
			delete(s.byID, uri)
		}
	}
	return entry, ok
}
