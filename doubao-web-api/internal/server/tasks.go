package server

import (
	"sync"
	"time"

	"github.com/volcengine/volcengine-go-sdk/service/arkruntime/model"
)

type videoTaskRecord struct {
	ID         string
	Model      string
	Status     string
	Ratio      string
	Duration   *int64
	VideoURL   string
	ETAText    string
	ETAMinutes int
	Error      *model.ContentGenerationError
	CreatedAt  int64
	UpdatedAt  int64
}

type videoTaskStore struct {
	mu    sync.RWMutex
	tasks map[string]*videoTaskRecord
}

func newVideoTaskStore() *videoTaskStore {
	return &videoTaskStore{tasks: make(map[string]*videoTaskRecord)}
}

func (s *videoTaskStore) create(id, modelName, ratio string, duration *int64) *videoTaskRecord {
	now := time.Now().Unix()
	task := &videoTaskRecord{
		ID:        id,
		Model:     modelName,
		Status:    model.StatusQueued,
		Ratio:     ratio,
		Duration:  duration,
		CreatedAt: now,
		UpdatedAt: now,
	}
	s.mu.Lock()
	s.tasks[id] = task
	s.mu.Unlock()
	return task
}

func (s *videoTaskStore) get(id string) (*videoTaskRecord, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	task, ok := s.tasks[id]
	return task, ok
}

func (s *videoTaskStore) update(id string, fn func(task *videoTaskRecord)) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	task, ok := s.tasks[id]
	if !ok {
		return false
	}
	fn(task)
	task.UpdatedAt = time.Now().Unix()
	return true
}
