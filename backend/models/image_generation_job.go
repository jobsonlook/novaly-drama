package models

import "time"

type ImageGenerationJob struct {
	ID           uint      `gorm:"primaryKey" json:"id"`
	ProjectID    uint      `gorm:"index" json:"projectId"`
	Type         string    `json:"type"` // character, scene, prop
	Status       string    `json:"status"` // pending, running, completed, failed
	Progress     int       `json:"progress"`
	Message      string    `json:"message"`
	TotalCount   int       `json:"totalCount"`
	DoneCount    int       `json:"doneCount"`
	Prompt       string    `json:"prompt,omitempty"`
	ResultJSON   string    `json:"-"`
	ErrorMessage string    `json:"error,omitempty"`
	InputJSON    string    `json:"-"`
	Dismissed    bool      `gorm:"default:false;index" json:"dismissed"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}
