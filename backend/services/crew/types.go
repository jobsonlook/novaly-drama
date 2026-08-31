package crew

type AssetItem struct {
	Name              string `json:"name"`
	Type              string `json:"type"` // character | scene | prop
	Description       string `json:"description"`
	VoicePrompt       string `json:"voicePrompt,omitempty"`
	Prompt            string `json:"prompt"`
	Priority          int    `json:"priority,omitempty"`
	ResourceID        uint   `json:"resourceId,omitempty"`
	ParentID          uint   `json:"parentId,omitempty"`
	ParentName        string `json:"parentName,omitempty"`
	ParentDescription string `json:"parentDescription,omitempty"`
	IsDerivative      bool   `json:"isDerivative,omitempty"`
	JobID             uint   `json:"jobId,omitempty"`
	Reused            bool   `json:"reused,omitempty"`
	Skipped           bool   `json:"skipped,omitempty"`
	Error             string `json:"error,omitempty"`
}

type DirectorResult struct {
	Plan       string      `json:"plan"`
	Characters []AssetItem `json:"characters"`
	Scenes     []AssetItem `json:"scenes"`
	Props      []AssetItem `json:"props"`
}

type ConsistencyResult struct {
	Assets []AssetItem `json:"assets"`
}

type StoryboardShot struct {
	Label          string   `json:"label"`
	Duration       int      `json:"duration"`
	Script         string   `json:"script"`
	CharacterNames []string `json:"characterNames"`
	SceneName      string   `json:"sceneName"`
	PropNames      []string `json:"propNames"`
}

type StoryboardResult struct {
	Shots []StoryboardShot `json:"shots"`
}

type QCIssue struct {
	Severity   string `json:"severity"` // high | medium | low
	Code       string `json:"code"`
	ShotID     uint   `json:"shotId,omitempty"`
	ShotIndex  int    `json:"shotIndex,omitempty"`
	ResourceID uint   `json:"resourceId,omitempty"`
	Message    string `json:"message"`
	Suggestion string `json:"suggestion"`
}

type QCReport struct {
	Score   string    `json:"score"` // A | B | C | D
	Summary string    `json:"summary"`
	Issues  []QCIssue `json:"issues"`
}

type ShotRefInfo struct {
	Kind         string `json:"kind"`
	Name         string `json:"name"`
	DisplayName  string `json:"displayName,omitempty"`
	ParentName   string `json:"parentName,omitempty"`
	ParentID     uint   `json:"parentId,omitempty"`
	ResourceID   uint   `json:"resourceId,omitempty"`
	IsDerivative bool   `json:"isDerivative,omitempty"`
	Prompt       string `json:"prompt,omitempty"`
	ParentPrompt string `json:"parentPrompt,omitempty"`
}

type ShotContext struct {
	ID       uint          `json:"id"`
	Index    int           `json:"shotIndex,omitempty"`
	Label    string        `json:"label"`
	Note     string        `json:"note,omitempty"`
	Script   string        `json:"script"`
	Duration int           `json:"duration,omitempty"`
	Refs     []ShotRefInfo `json:"refs"`
}
