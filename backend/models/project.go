package models

import (
	"time"

	"gorm.io/gorm"
)

type Project struct {
	ID             uint           `gorm:"primaryKey" json:"id"`
	Title          string         `json:"title"`
	EpisodeCount   int            `json:"episodeCount"`
	Kind           string         `gorm:"default:script" json:"kind"` // script | novel
	Genre          string         `json:"genre"`
	Synopsis       string         `json:"synopsis"`
	VisualManual   string         `json:"visualManual"`
	DirectorManual string         `json:"directorManual"`
	Style          string         `json:"style"`
	VideoRatio     string         `gorm:"default:16:9" json:"videoRatio"`
	// StoryboardPace: fine ≈ 第1集细切（对白/动作拍切开）；packed ≈ 第2集 10 秒打包。
	StoryboardPace string         `gorm:"default:fine" json:"storyboardPace"`
	CreatedAt      time.Time      `json:"createdAt"`
	UpdatedAt      time.Time      `json:"updatedAt"`
	DeletedAt      gorm.DeletedAt `gorm:"index" json:"deletedAt,omitempty"`
	Episodes       []Episode      `gorm:"foreignKey:ProjectID" json:"episodes,omitempty"`
	Resources      []Resource     `gorm:"foreignKey:ProjectID" json:"resources,omitempty"`
}

type EpisodeAsset struct {
	Name string `json:"name"`
	Type string `json:"type"` // character | scene | prop
}

type Episode struct {
	ID           uint           `gorm:"primaryKey" json:"id"`
	ProjectID    uint           `gorm:"index" json:"projectId"`
	Number       int            `json:"number"`
	Title        string         `json:"title"`
	Script       string         `json:"script"`
	DirectorPlan string         `json:"directorPlan"`
	AssetsJSON   string         `gorm:"column:assets_json" json:"-"`
	CreatedAt    time.Time      `json:"createdAt"`
	UpdatedAt    time.Time      `json:"updatedAt"`
	Shots        []Shot         `gorm:"foreignKey:EpisodeID" json:"shots,omitempty"`
	ShotTotal    int            `json:"shotTotal,omitempty" gorm:"-"`
	Assets       []EpisodeAsset `gorm:"-" json:"assets,omitempty"`
	CrewStatus   string         `gorm:"-" json:"crewStatus,omitempty"`
	CrewStage    string         `gorm:"-" json:"crewStage,omitempty"`
}

type Shot struct {
	ID                    uint             `gorm:"primaryKey" json:"id"`
	EpisodeID             uint             `gorm:"index" json:"episodeId"`
	SortOrder             int              `json:"sortOrder"`
	Label                 string           `json:"label"`
	Script                string           `json:"script"`
	Note                  string           `json:"note"`
	VisualStyle           string           `json:"visualStyle"`
	ImageRefs             string           `json:"imageRefs"`
	Duration              int              `gorm:"default:10" json:"duration"`
	Resolution            string           `gorm:"default:720p" json:"resolution"`
	VideoModelID          *uint            `json:"videoModelId"`
	RefsJSON              string           `json:"-"`
	CharacterRefsJSON     string           `json:"-"`
	CharacterIDsJSON      string           `json:"-"` // legacy
	SceneID               *uint            `json:"sceneId"`
	VideoURL              string           `json:"videoUrl"`
	VideoTaskID           string           `json:"videoTaskId"`
	VideoETA              string           `json:"videoEta"`
	ActiveVideoResourceID *uint            `json:"activeVideoResourceId,omitempty"`
	Status                string           `gorm:"default:draft" json:"status"`
	ErrorMessage          string           `json:"errorMessage"`
	PositioningPrompt     string           `json:"positioningPrompt,omitempty"`
	PositioningRefsJSON   string           `json:"-"`
	MotionGridPrompt      string           `json:"motionGridPrompt,omitempty"`
	MotionGridRefsJSON    string           `json:"-"`
	CreatedAt             time.Time        `json:"createdAt"`
	UpdatedAt             time.Time        `json:"updatedAt"`
	CharacterRefs         []CharacterRef   `gorm:"-" json:"characterRefs"`
	Refs                  []ShotRef        `gorm:"-" json:"refs"`
	PositioningRefs       []ResourceGenRef `gorm:"-" json:"positioningRefs,omitempty"`
	MotionGridRefs        []ResourceGenRef `gorm:"-" json:"motionGridRefs,omitempty"`
}

type ShotRef struct {
	Kind    string `json:"kind"` // character, scene, prop, other
	ID      uint   `json:"id"`
	Variant string `json:"variant,omitempty"` // stylized, original (character, scene & other)
	Label   string `json:"label,omitempty"`   // optional display alias for 图N为xxx
}

type CharacterRef struct {
	ID      uint   `json:"id"`
	Variant string `json:"variant"` // stylized, original
}

type ProjectSummary struct {
	ID             uint       `json:"id"`
	Title          string     `json:"title"`
	EpisodeCount   int        `json:"episodeCount"`
	Kind           string     `json:"kind"`
	Genre          string     `json:"genre"`
	Synopsis       string     `json:"synopsis"`
	VisualManual   string     `json:"visualManual"`
	DirectorManual string     `json:"directorManual"`
	Style          string     `json:"style"`
	VideoRatio     string     `json:"videoRatio"`
	StoryboardPace string     `json:"storyboardPace"`
	ShotCount      int        `json:"shotCount"`
	CreatedAt      time.Time  `json:"createdAt"`
	UpdatedAt      time.Time  `json:"updatedAt"`
	DeletedAt      *time.Time `json:"deletedAt,omitempty"`
}

type ProjectDTO struct {
	ID             uint       `json:"id"`
	Title          string     `json:"title"`
	EpisodeCount   int        `json:"episodeCount"`
	Kind           string     `json:"kind"`
	Genre          string     `json:"genre"`
	Synopsis       string     `json:"synopsis"`
	VisualManual   string     `json:"visualManual"`
	DirectorManual string     `json:"directorManual"`
	Style          string     `json:"style"`
	VideoRatio     string     `json:"videoRatio"`
	StoryboardPace string     `json:"storyboardPace"`
	Episodes       []Episode  `json:"episodes"`
	Resources      []Resource `json:"resources"`
	CreatedAt      time.Time  `json:"createdAt"`
	UpdatedAt      time.Time  `json:"updatedAt"`
}
