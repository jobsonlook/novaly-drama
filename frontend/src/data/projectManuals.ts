export type VisualManual = {
  id: string
  name: string
  desc: string
  stylePrompt: string
  videoStylePrompt: string
  tone: string
}

export type DirectorManual = {
  id: string
  name: string
  desc: string
}

export const visualManuals: VisualManual[] = [
  { id: '2d-90s-anime', name: '90年代日式动画', desc: '手绘平涂 · 怀旧暖调', tone: '#c9a96e', stylePrompt: '90年代日式动画风格，手绘平涂，清晰流畅线条，柔和暖调，电影感光影层次，怀旧治愈，赛璐璐上色', videoStylePrompt: '90年代日式动画，手绘赛璐璐，柔和暖调，电影风格，清晰线条，怀旧质感' },
  { id: '2d-guofeng', name: '国风二次元新国潮', desc: '古风仙侠 · 新国潮美学', tone: '#c45c4a', stylePrompt: '国风二次元新国潮，古风仙侠美学，日式动画渲染，赛璐璐平涂结合数字光影，东方留白与诗意构图', videoStylePrompt: '国风二次元动画，赛璐璐平涂，新国潮东方美学，电影风格，色彩鲜明，细腻笔触' },
  { id: '2d-flat', name: '2D扁平风', desc: '纯色色块 · 简约现代', tone: '#5b8def', stylePrompt: '2D扁平风，纯色色块，无阴影无渐变，简洁线条，明快现代简约插画', videoStylePrompt: '2D扁平风格，几何造型，纯色色块，无阴影，简洁线条，现代简约' },
  { id: '3d-cute', name: '3D可爱潮流', desc: '动画渲染 · 治愈明亮', tone: '#e8a0b8', stylePrompt: '3D可爱潮流动画渲染，轮廓清晰，材质细腻，温暖明亮，治愈电影感', videoStylePrompt: '3D动画渲染，赛璐珞质感，电影级光影，温暖色调，高细节材质，清晰轮廓线' },
  { id: '3d-guofeng', name: '国风3D', desc: '三维国风 · 电影光影', tone: '#2f6f5e', stylePrompt: '国风3D，高精度建模与PBR材质，青绿朱红靛蓝金黄传统色盘，电影级体积光与东方意境', videoStylePrompt: '国风3D渲染，PBR材质，体积光，东方美学，典雅大气，电影风格' },
  { id: '3d-clay', name: '定格黏土', desc: '手工肌理 · 怀旧治愈', tone: '#d08a4c', stylePrompt: '定格黏土动画质感，黏土肌理与手指压痕可见，温暖柔和怀旧治愈，手工模型光影', videoStylePrompt: '定格动画黏土风格，黏土肌理，手指压痕，暖色调，柔和浅景深，奇幻3D卡通' },
  { id: 'real-ancient', name: '真人古风', desc: '写实摄影 · 东方影视', tone: '#8a6a4a', stylePrompt: '真人古风写实影视质感，古代宅邸宫殿服饰，35mm胶片颗粒，冷中带暖，杜绝现代元素', videoStylePrompt: '古风写实摄影，电影风格，强对比度，极致细节' },
  { id: 'real-urban', name: '真人都市', desc: '现代写实 · 电影胶片', tone: '#4a5568', stylePrompt: '真人现代都市写实，电影胶片颗粒，自然光影，低对比度调色，纯实拍质感', videoStylePrompt: '真人都市电影摄影，真人实拍质感，当代中国都市，电影级色彩科学，自然光与实用光源调度，浅景深，手持呼吸感或稳定器流动，电影颗粒质感，视频动态优化，非CG非渲染' },
]

export const directorManuals: DirectorManual[] = [
  { id: 'comedy', name: '喜剧搞笑', desc: '预期违背与节奏笑点' },
  { id: 'coming-of-age', name: '青春成长', desc: '身份迷茫与第一次选择' },
  { id: 'family', name: '家庭温情', desc: '日常细节与代际理解' },
  { id: 'historical', name: '历史史诗', desc: '个人命运与时代洪流' },
  { id: 'horror', name: '恐怖灵异', desc: '未见之时的压迫感' },
  { id: 'mystery', name: '悬疑推理', desc: '公平线索与可回溯反转' },
  { id: 'xianxia', name: '仙侠奇幻', desc: '奇观服务人物关系' },
  { id: 'workplace', name: '都市职场', desc: '利益、体面与潜台词' },
  { id: 'romance', name: '甜宠言情', desc: '具体互动里的心动' },
  { id: 'scifi', name: '科幻末日', desc: '规则、生存与人性' },
  { id: 'action', name: '热血动作', desc: '准备—爆发—余韵' },
  { id: 'psychological', name: '心理剧', desc: '内心外化为空间隐喻' },
]

export function visualManualById(id: string) {
  return visualManuals.find(v => v.id === id)
}

export function directorManualById(id: string) {
  return directorManuals.find(v => v.id === id)
}

export function visualManualName(id?: string) {
  return visualManualById(id || '')?.name || ''
}

export function visualManualVideoPrompt(id?: string) {
  return visualManualById(id || '')?.videoStylePrompt || ''
}
