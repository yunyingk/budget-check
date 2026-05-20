package main

import (
	"sync"
	"time"
)

type DimEntry struct {
	DimCode  string
	DimType  string
	NodeName string
	BudgetID string
}

type Store struct {
	mu     sync.RWMutex
	data   map[string]DimEntry
	updatedAt time.Time
}

func NewStore() *Store {
	return &Store{
		data: make(map[string]DimEntry),
	}
}

func (s *Store) Put(key string, entry DimEntry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[key] = entry
}

func (s *Store) BatchPut(entries []DimEntry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range entries {
		s.data[e.DimCode] = e
	}
	s.updatedAt = time.Now()
}

func (s *Store) Get(dimCode string) (DimEntry, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.data[dimCode]
	return e, ok
}

func (s *Store) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data = make(map[string]DimEntry)
}

func (s *Store) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.data)
}

func (s *Store) UpdatedAt() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.updatedAt
}