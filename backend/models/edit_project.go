package models

import "time"

// EditProject stores the browser editor timeline as JSON. Media stays in the
// existing Resource/Shot tables; the timeline only keeps references and edits.
type EditProject struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	ProjectID uint      `gorm:"uniqueIndex:idx_edit_project_episode" json:"projectId"`
	EpisodeID uint      `gorm:"uniqueIndex:idx_edit_project_episode" json:"episodeId"`
	DataJSON  string    `gorm:"type:text" json:"-"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}
