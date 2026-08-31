package models

import "time"

// CrewJob is one episode-level multi-agent production run.
type CrewJob struct {
	ID              uint      `gorm:"primaryKey" json:"id"`
	ProjectID       uint      `gorm:"index" json:"projectId"`
	EpisodeID       uint      `gorm:"index" json:"episodeId"`
	Status          string    `json:"status"` // running | waiting_review | completed | failed
	Stage           string    `json:"stage"`  // screenwriter | director | consistency | assets | storyboard | qc
	SourceScript    string    `json:"sourceScript"`
	ScriptDraft     string    `json:"scriptDraft"`
	DirectorPlan    string    `json:"directorPlan"`
	AssetsJSON      string    `gorm:"column:assets_json" json:"-"`
	QCReportJSON    string    `gorm:"column:qc_report_json" json:"-"`
	ImageJobIDsJSON string    `gorm:"column:image_job_ids_json" json:"-"`
	ShotIDsJSON     string    `gorm:"column:shot_ids_json" json:"-"`
	ChatJSON        string    `gorm:"column:chat_json" json:"-"`
	ShotMode        string    `json:"shotMode"` // replace | append | from
	FromShotID      uint      `json:"fromShotId,omitempty"`
	ErrorMessage    string    `json:"errorMessage"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
}
