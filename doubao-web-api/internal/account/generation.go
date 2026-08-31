package account

import (
	"database/sql"
	"encoding/json"
	"strings"
	"time"
)

const (
	GenerationKindImage = "image"
	GenerationKindVideo = "video"
)

// Generation is one recorded image/video generation for the admin history UI.
type Generation struct {
	ID          int64
	Kind        string
	Prompt      string
	Model       string
	Status      string
	Images      []string // image resource URLs (output or reference)
	ResultURL   string   // video URL when kind=video
	TaskID      string
	AccountID   int64
	AccountName string
	CreatedAt   time.Time
}

// GenerationInput is used to append a history row.
type GenerationInput struct {
	Kind        string
	Prompt      string
	Model       string
	Status      string
	Images      []string
	ResultURL   string
	TaskID      string
	AccountID   int64
	AccountName string
}

// AddGeneration appends a generation history record.
func (s *Store) AddGeneration(in GenerationInput) (Generation, error) {
	if s == nil {
		return Generation{}, ErrNotFound
	}
	kind := strings.TrimSpace(in.Kind)
	if kind == "" {
		kind = GenerationKindImage
	}
	status := strings.TrimSpace(in.Status)
	if status == "" {
		status = "succeeded"
	}
	images := in.Images
	if images == nil {
		images = []string{}
	}
	imagesJSON, err := json.Marshal(images)
	if err != nil {
		return Generation{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now().UTC().Format(time.RFC3339)
	res, err := s.db.Exec(`
INSERT INTO generation_history (
	kind, prompt, model, status, images_json, result_url, task_id, account_id, account_name, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		kind,
		in.Prompt,
		strings.TrimSpace(in.Model),
		status,
		string(imagesJSON),
		strings.TrimSpace(in.ResultURL),
		strings.TrimSpace(in.TaskID),
		in.AccountID,
		strings.TrimSpace(in.AccountName),
		now,
	)
	if err != nil {
		return Generation{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Generation{}, err
	}
	return s.getGenerationUnlocked(id)
}

// ListGenerations returns recent generation history (newest first).
func (s *Store) ListGenerations(limit int) ([]Generation, error) {
	if limit <= 0 {
		limit = 50
	}
	return s.ListGenerationsPage(0, limit)
}

// CountGenerations returns total generation_history rows.
func (s *Store) CountGenerations() (int, error) {
	if s == nil {
		return 0, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM generation_history`).Scan(&n)
	return n, err
}

// ListGenerationsPage returns a page of generation history (newest first).
// offset is 0-based; limit defaults to 20 and is capped at 100.
func (s *Store) ListGenerationsPage(offset, limit int) ([]Generation, error) {
	if s == nil {
		return nil, nil
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	rows, err := s.db.Query(`
SELECT id, kind, prompt, model, status, images_json, result_url, task_id, account_id, account_name, created_at
FROM generation_history
ORDER BY id DESC
LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Generation
	for rows.Next() {
		g, err := scanGeneration(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

func (s *Store) getGenerationUnlocked(id int64) (Generation, error) {
	row := s.db.QueryRow(`
SELECT id, kind, prompt, model, status, images_json, result_url, task_id, account_id, account_name, created_at
FROM generation_history WHERE id = ?`, id)
	g, err := scanGeneration(row)
	if err == sql.ErrNoRows {
		return Generation{}, ErrNotFound
	}
	return g, err
}

func scanGeneration(row rowScanner) (Generation, error) {
	var (
		g          Generation
		imagesJSON string
		createdAt  string
	)
	if err := row.Scan(
		&g.ID, &g.Kind, &g.Prompt, &g.Model, &g.Status,
		&imagesJSON, &g.ResultURL, &g.TaskID, &g.AccountID, &g.AccountName, &createdAt,
	); err != nil {
		return Generation{}, err
	}
	g.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	if imagesJSON != "" {
		_ = json.Unmarshal([]byte(imagesJSON), &g.Images)
	}
	if g.Images == nil {
		g.Images = []string{}
	}
	return g, nil
}
