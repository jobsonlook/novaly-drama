package models

import "time"

// AutoVideoRun is a durable, sequential video generation run for one episode.
type AutoVideoRun struct {
	ID            uint               `gorm:"primaryKey" json:"id"`
	ProjectID     uint               `gorm:"index" json:"projectId"`
	EpisodeID     uint               `gorm:"index" json:"episodeId"`
	StartShotID   uint               `json:"startShotId"`
	CurrentShotID uint               `json:"currentShotId"`
	Status        string             `gorm:"index" json:"status"` // running, pause_requested, paused, completed, cancelled
	Stage         string             `json:"stage"`               // reviewing, generating, extracting_frame
	PassedCount   int                `json:"passedCount"`
	TotalCount    int                `json:"totalCount"`
	MaxRetries    int                `gorm:"default:2" json:"maxRetries"`
	PauseReason   string             `json:"pauseReason"`
	ErrorMessage  string             `json:"errorMessage"`
	CreatedAt     time.Time          `json:"createdAt"`
	UpdatedAt     time.Time          `json:"updatedAt"`
	Items         []AutoVideoRunItem `gorm:"foreignKey:RunID" json:"items,omitempty"`
}

type AutoVideoRunItem struct {
	ID                  uint      `gorm:"primaryKey" json:"id"`
	RunID               uint      `gorm:"index" json:"runId"`
	ShotID              uint      `gorm:"index" json:"shotId"`
	SortOrder           int       `json:"sortOrder"`
	Status              string    `json:"status"` // pending, generating, reviewing, passed, failed, paused
	Attempts            int       `json:"attempts"`
	VideoTaskID         string    `json:"videoTaskId,omitempty"`
	CandidateResourceID *uint     `json:"candidateResourceId,omitempty"`
	CandidateVideoURL   string    `gorm:"-" json:"candidateVideoUrl,omitempty"`
	AcceptedResourceID  *uint     `json:"acceptedResourceId,omitempty"`
	ReviewJSON          string    `json:"-"`
	Review              any       `gorm:"-" json:"review,omitempty"`
	FailureSummary      string    `json:"failureSummary"`
	CreatedAt           time.Time `json:"createdAt"`
	UpdatedAt           time.Time `json:"updatedAt"`
}

// AppSetting stores local-only integration settings. Values are never returned raw.
type AppSetting struct {
	Key       string    `gorm:"primaryKey" json:"key"`
	Value     string    `json:"-"`
	UpdatedAt time.Time `json:"updatedAt"`
}
