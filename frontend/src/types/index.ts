export type CharacterRef = { id: number; variant: 'stylized' | 'original' }
export type ShotRef = { kind: 'character' | 'scene' | 'prop' | 'other'; id: number; variant?: 'stylized' | 'original'; label?: string }
export type Shot = {
  id: number; episodeId: number; sortOrder: number; label: string; script: string
  note: string
  visualStyle: string; imageRefs: string; duration: number; resolution: string
  videoModelId: number | null
  characterRefs: CharacterRef[]; refs: ShotRef[]; sceneId: number | null
  videoUrl: string; videoEta?: string; status: string; errorMessage: string
  activeVideoResourceId?: number | null
  positioningPrompt?: string
  positioningRefs?: ResourceGenRef[]
  motionGridPrompt?: string
  motionGridRefs?: ResourceGenRef[]
  createdAt?: string; updatedAt?: string
}
export type Episode = {
  id: number
  projectId: number
  number: number
  title: string
  script?: string
  directorPlan?: string
  shots: Shot[]
  shotTotal?: number
  assets?: CrewAsset[]
  crewStatus?: string
  crewStage?: string
}

export type CrewAsset = {
  name: string
  type: 'character' | 'scene' | 'prop' | string
  description: string
  prompt: string
  voicePrompt?: string
  priority?: number
  resourceId?: number
  jobId?: number
  reused?: boolean
  skipped?: boolean
  error?: string
}

export type CrewQCIssue = {
  severity: 'high' | 'medium' | 'low' | string
  code: string
  shotId?: number
  shotIndex?: number
  resourceId?: number
  message: string
  suggestion: string
}

export type CrewQCReport = {
  score: string
  summary: string
  issues: CrewQCIssue[]
}

export type CrewChatMessage = {
  id: string
  role: 'user' | 'assistant' | string
  name?: string
  content: string
  action?: string
  createdAt?: number
}

export type CrewStage = 'screenwriter' | 'director' | 'consistency' | 'assets' | 'storyboard' | 'qc'

export type CrewJob = {
  id: number
  projectId: number
  episodeId: number
  status: 'running' | 'waiting_review' | 'completed' | 'failed' | string
  stage: CrewStage | string
  sourceScript: string
  scriptDraft: string
  directorPlan: string
  assets: CrewAsset[]
  qc?: CrewQCReport | null
  chat?: CrewChatMessage[]
  imageJobs: ImageGenJobView[]
  shotIds: number[]
  shotMode?: string
  errorMessage?: string
  createdAt?: string
  updatedAt?: string
}
export type ResourceGenRef = {
  id: number
  variant?: string
  kind?: string
  label?: string
  imageUrl?: string
}
export type Resource = {
  id: number; projectId: number
  type: 'character' | 'scene' | 'prop' | 'video' | 'other'
  source: 'ai' | 'upload' | ''
  name: string; description: string; remark?: string
  sceneGridShapeLegend?: string
  voicePrompt?: string
  imageUrl: string; stylizedImageUrl: string; videoUrl: string
  shotId?: number | null; duration?: number; resolution?: string
  genScript?: string; genVisualStyle?: string; genProjectStyle?: string
  genModelName?: string; genModelId?: string; genProviderName?: string
  genPrompt?: string
  genType?: 'character' | 'scene' | 'prop' | 'positioning' | 'scene_grid' | 'motion_grid' | 'motion_grid_cell' | 'scene_grid_cell' | string
  genRefs?: ResourceGenRef[]
  gridId?: number
  gridCell?: number
  isGroupPrimary?: boolean
  parentId?: number | null
  parentName?: string
  deriveCount?: number
  deletedAt?: string
  createdAt?: string
}
export type ProjectSummary = {
  id: number
  title: string
  episodeCount: number
  kind?: string
  genre?: string
  synopsis?: string
  visualManual?: string
  directorManual?: string
  style: string
  videoRatio: string
  storyboardPace?: string
  shotCount: number
  createdAt?: string
  updatedAt?: string
  deletedAt?: string
}
export type Project = {
  id: number
  title: string
  episodeCount: number
  kind?: string
  genre?: string
  synopsis?: string
  visualManual?: string
  directorManual?: string
  style: string
  videoRatio: string
  storyboardPace?: string
  episodes: Episode[]
  resources: Resource[]
}
export type AIModel = { id: number; providerId: number; name: string; modelId: string; capability: 'text' | 'image' | 'video'; enabled: boolean; isDefault: boolean }
export type Provider = { id: number; name: string; slug: string; baseUrl: string; apiKeyMasked: string; hasApiKey: boolean; enabled: boolean; models: AIModel[] }

export type SceneReference = {
  key: string
  source: 'upload' | 'resource'
  imageData?: string
  resourceId?: number
  kind?: 'character' | 'scene' | 'prop' | 'other'
  variant?: 'stylized' | 'original'
  previewUrl: string
  label: string
}
export type ResourceDisplayEntry = { kind: 'resource'; resource: Resource }
export type ConfirmModalState = {
  title: string; message: string; confirmText: string; danger?: boolean
  onConfirm: () => void | Promise<void>
} | null

export type StylizeModalState = {
  resourceId: number
  resourceName: string
  type: 'character' | 'scene' | 'other' | 'prop'
  prompt: string
} | null

export type PositioningModalState = {
  shotId: number
  shotLabel: string
  prompt: string
  /** Draft/resource hydration runs after the modal is already visible. */
  initializing?: boolean
  analyzing: boolean
  submitting: boolean
  submittingStep?: 'skeleton' | 'final'
  /** Stick-figure blocking sketch waiting for user approval */
  skeleton?: { url: string; resourceId?: number }
  /** Full prompt actually sent to the image model (from a completed job) */
  lastSentPrompt?: string
  /** Completed job candidates when opened from the task panel */
  results?: { url: string; resourceId?: number; label?: string }[]
} | null

export type MotionGridModalState = {
  shotId: number
  shotLabel: string
  prompt: string
  analyzing: boolean
  submitting: boolean
  /** Completed job candidates when opened from the task panel */
  results?: { url: string; resourceId?: number; label?: string }[]
} | null

export type SceneGridModalState = {
  /** Source scene resource the grid is built from (optional) */
  resourceId?: number
  name: string
  prompt: string
  submitting: boolean
  overheadSubmitting?: boolean
  /** Floor plan preview URLs are set but browser images still loading (local UI only) */
  overheadPreviewLoading?: boolean
  /** Shape-to-object legend used by overhead sketch and carried into 9-grid prompt */
  overheadShapeLegend?: string
  /** AI is drafting the shape legend from scene description */
  overheadShapeLegendAnalyzing?: boolean
  /** Approved bird-eye layout sketch used to lock room geometry before generating the grid */
  overheadSketch?: { url: string; resourceId?: number; imageData?: string }
  /** Candidate overhead sketches generation count (each candidate is one 16:9 line sketch) */
  overheadSketchCandidateCount?: number
  /** Candidate overhead sketches to let user pick the correct one */
  overheadSketchCandidates?: { url: string; resourceId?: number; imageData?: string }[]
  /** Completed job candidates when opened from the task panel */
  results?: { url: string; resourceId?: number; label?: string }[]
} | null

export type SceneReverseModalState = {
  /** Source scene plate the reverse shot is built from */
  resourceId?: number
  /** User-selected scene 9-grid; overhead + opposite cells are taken from this */
  gridId?: number
  name: string
  prompt: string
  submitting: boolean
  submittingStep?: 'skeleton' | 'final'
  /** Reverse-camera stick-figure frame waiting for user approval */
  skeleton?: { url: string; resourceId?: number }
  lastSentPrompt?: string
  /** Completed reverse-plate candidates when opened from the task panel */
  results?: { url: string; resourceId?: number; label?: string }[]
} | null

export type ScenePanoramaModalState = {
  /** Source scene plate expanded into 360° */
  resourceId?: number
  /** Preferred scene 9-grid whose cells drive the unwrap */
  gridId?: number
  name: string
  prompt: string
  submitting: boolean
  results?: { url: string; resourceId?: number; label?: string }[]
} | null

export type PanoramaViewerState = {
  url: string
  label: string
  /** Degrees; Novaly safe-seam front defaults to -90 */
  initialYawDeg?: number
} | null

export type DirectorDeskModalState = {
  /** Scoped localStorage key inside the desk iframe */
  instanceId: string
  /** Absolute or same-origin URL / data URL for the panorama */
  panoramaUrl: string
  panoramaName: string
  panoramaResourceId?: number
  /** When opened from 站位图 flow, captures become the skeleton */
  purpose: 'positioning' | 'browse'
  shotId?: number
} | null

export type EditResourceModalState = {
  resourceId: number
  type: Resource['type']
  name: string
  description: string
  genPrompt: string
  voicePrompt: string
  remark: string
} | null

export type ModelFormState = {
  providerId: number
  providerSlug: string
  capability: 'text' | 'image' | 'video'
  editingId?: number
  name: string
  modelId: string
} | null

export type ModelPreset = { name: string; modelId: string }

export type PromptPreviewState = {
  shotId: number
  prompt: string
  refImages: { index: number; label: string; kind: string; name: string; variant?: string }[]
  modelId: string; modelName: string; ratio: string; duration: number; resolution: string
} | null

export type ImagePreviewState = { url: string; label: string; selectUrl?: string } | null

export type ImageGenProgressState = {
  progress: number
  message: string
  doneCount: number
  totalCount: number
} | null

export type ImageGenJobView = {
  id: number
  projectId: number
  type: 'character' | 'scene' | 'prop' | 'positioning' | 'scene_grid' | 'motion_grid' | string
  status: 'pending' | 'running' | 'completed' | 'failed'
  progress: number
  message: string
  doneCount: number
  totalCount: number
  name: string
  prompt?: string
  description?: string
  resourceRefs?: ResourceGenRef[]
  error?: string
  images?: { url: string; resourceId?: number }[]
  resources?: Resource[]
  shotId?: number
  targetResourceId?: number
}

export type StylizeJobView = {
  id: number
  resourceId: number
  name: string
  status: 'running' | 'completed' | 'failed'
  message: string
  error?: string
}
