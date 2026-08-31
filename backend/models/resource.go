package models

import (
	"time"

	"gorm.io/gorm"
)

type Resource struct {
	ID                uint             `gorm:"primaryKey" json:"id"`
	ProjectID         uint             `gorm:"index" json:"projectId"`
	ParentID          *uint            `gorm:"index" json:"parentId,omitempty"` // 底模资源；非空则为衍生（换装/时段等）
	ParentName        string           `gorm:"-" json:"parentName,omitempty"`
	DeriveCount       int              `gorm:"-" json:"deriveCount,omitempty"`
	Type              string           `json:"type"`   // character, scene, prop, video, other
	Source            string           `json:"source"` // ai, upload
	Name              string           `json:"name"`
	Description       string           `json:"description"`
	VoicePrompt       string           `json:"voicePrompt"` // 角色音色，各分镜视频共用同一句
	Remark                string           `json:"remark"`
	SceneGridShapeLegend  string           `json:"sceneGridShapeLegend,omitempty"` // 场景9宫格平面图形语义对照，绑定底模场景
	ImagePath         string           `json:"-"`
	StylizedImagePath string           `json:"-"`
	VideoPath         string           `json:"-"`
	ShotID            *uint            `gorm:"index" json:"shotId,omitempty"`
	Duration          int              `json:"duration,omitempty"`
	Resolution        string           `json:"resolution,omitempty"`
	GenScript         string           `json:"genScript,omitempty"`
	GenVisualStyle    string           `json:"genVisualStyle,omitempty"`
	GenProjectStyle   string           `json:"genProjectStyle,omitempty"`
	GenModelName      string           `json:"genModelName,omitempty"`
	GenModelID        string           `json:"genModelId,omitempty"`
	GenProviderName   string           `json:"genProviderName,omitempty"`
	GenPrompt         string           `json:"genPrompt,omitempty"`
	GenType           string           `json:"genType,omitempty"`  // character, scene, prop, positioning, scene_grid, scene_reverse, scene_reverse_skeleton, scene_panorama, motion_grid, motion_grid_cell
	GridID            uint             `json:"gridId,omitempty"`   // for grid cells: parent 9-grid resource id
	GridCell          int              `json:"gridCell,omitempty"` // 1..9 cell index; 0 = not a cell
	GenRefsJSON       string           `json:"-"`
	GenRefs           []ResourceGenRef `gorm:"-" json:"genRefs,omitempty"`
	ImageURL          string           `gorm:"-" json:"imageUrl"`
	StylizedImageURL  string           `gorm:"-" json:"stylizedImageUrl"`
	VideoURL          string           `gorm:"-" json:"videoUrl"`
	IsGroupPrimary    bool             `gorm:"default:false" json:"isGroupPrimary"`
	CreatedAt         time.Time        `json:"createdAt"`
	UpdatedAt         time.Time        `json:"updatedAt"`
	DeletedAt         gorm.DeletedAt   `gorm:"index" json:"deletedAt,omitempty"`
}

// ResourceGenRef records a reference image used when AI-generating this resource.
type ResourceGenRef struct {
	ID       uint   `json:"id"`
	Variant  string `json:"variant,omitempty"`
	Kind     string `json:"kind,omitempty"`
	Label    string `json:"label,omitempty"`
	ImageURL string `json:"imageUrl,omitempty"`
}
