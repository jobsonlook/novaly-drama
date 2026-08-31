package crew

import (
	"fmt"
	"strings"

	"novaly/backend/models"
	"novaly/backend/services"
)

type visualStyle struct {
	ID         string
	Title      string
	Gene       string
	VideoTag   string // Toonflow Seedance 2.0 中文风格标签，只用于视频，不含角色肤色发丝
	Forbid     string
	SkinF      string
	SkinM      string
	BodyF      string
	BodyM      string
	HairF      string
	HairM      string
	Clothes    string
	Background string
	Light      string
	FaceRender string
	World      string
	SceneLook  string
	PropLook   string
}

func resolveVisualStyle(project models.Project) visualStyle {
	key := strings.TrimSpace(project.VisualManual)
	if key == "" {
		key = inferVisualKey(project.Style)
	}
	spec, ok := visualStyles[key]
	if !ok {
		spec = visualStyles["2d-90s-anime"]
	}
	if extra := strings.TrimSpace(project.Style); extra != "" && extra != spec.Gene {
		spec.Gene = spec.Gene + "。项目补充画风：" + clipRunes(extra, 600)
	}
	return spec
}

// VideoLookPack is the Toonflow Seedance 2.0 style block for video prompts:
// handbook video tag + optional project/shot overlay + aesthetic forbids.
// Quality/stability lines are appended later in BuildVideoPrompt.
func VideoLookPack(project models.Project, shotVisualStyle string) string {
	key := strings.TrimSpace(project.VisualManual)
	if key == "" {
		key = inferVisualKey(project.Style)
	}
	spec, ok := visualStyles[key]
	if !ok {
		spec = visualStyles["2d-90s-anime"]
	}
	tag := strings.TrimSpace(spec.VideoTag)
	if tag == "" {
		tag = strings.TrimSpace(spec.Gene)
	}
	var b strings.Builder
	b.WriteString(tag)
	extra := strings.TrimSpace(project.Style)
	if extra != "" && extra != spec.Gene && extra != tag {
		b.WriteString("。项目补充画风：")
		b.WriteString(clipRunes(extra, 600))
	}
	if f := strings.TrimSpace(spec.Forbid); f != "" {
		b.WriteString("。")
		b.WriteString(f)
	}
	overlay := strings.TrimSpace(shotVisualStyle)
	if overlay != "" && overlay != extra && overlay != tag && overlay != spec.Gene {
		b.WriteString("。本镜补充：")
		b.WriteString(clipRunes(overlay, 240))
	}
	return b.String()
}

func inferVisualKey(style string) string {
	s := strings.ToLower(style)
	switch {
	case strings.Contains(s, "古风") && (strings.Contains(s, "真人") || strings.Contains(s, "写实")):
		return "real-ancient"
	case strings.Contains(s, "真人") || strings.Contains(s, "写实") || strings.Contains(s, "胶片"):
		return "real-urban"
	case strings.Contains(s, "黏土"):
		return "3d-clay"
	case strings.Contains(s, "国风") && strings.Contains(s, "3d"):
		return "3d-guofeng"
	case strings.Contains(s, "3d") || strings.Contains(s, "可爱潮流"):
		return "3d-cute"
	case strings.Contains(s, "扁平"):
		return "2d-flat"
	case strings.Contains(s, "国风") || strings.Contains(s, "仙侠"):
		return "2d-guofeng"
	default:
		return "2d-90s-anime"
	}
}

var visualStyles = map[string]visualStyle{
	"2d-90s-anime": {
		ID:         "2d-90s-anime",
		Title:      "90年代复古日系动画",
		Gene:       "90年代日式动画风格，手绘平涂，清晰流畅线条，柔和暖调，电影感光影层次，怀旧治愈，赛璐璐上色",
		VideoTag:   "90年代日式动画，手绘赛璐璐，柔和暖调，电影风格，清晰线条，怀旧质感",
		Forbid:     "严禁真人写实、摄影、3D渲染、CGI、数字化锐利边缘、过饱和荧光色",
		SkinF:      "暖调米白或冷白皮，平涂上色，柔和光泽，手绘质感，锁骨与颈线清晰",
		SkinM:      "健康暖调肤色，平涂上色，自然光泽，手绘质感",
		BodyF:      "默认 160-168cm，6.5-7.5 头身，纤细肩线，体态自然优雅",
		BodyM:      "默认 175-185cm，7-8 头身，宽肩窄腰，身姿挺拔",
		HairF:      "纯黑/深蓝/深棕长发，及肩或及腰，发丝层次分明，自然散发，无发饰",
		HairM:      "纯黑或深棕短发至中长发，侧分或自然散落，发丝层次分明，无发冠",
		Clothes:    "由身份决定日常默认态：学生→90年代校服；上班族→职业便装；休闲→复古连衣裙/衬衫牛仔裤；特殊职业→对应工装。低饱和柔和色，无复杂花纹。禁止内衣打底。",
		Background: "纯净暖调米白或中性灰背景",
		Light:      "柔和电影光，前方主光+双侧补光，无硬阴影",
		FaceRender: "面容细腻渲染，发丝细腻渲染，手绘块面阴影",
		World:      "现代或贴近角色年代的二次元日常世界，不要科幻霓虹",
		SceneLook:  "手绘赛璐璐场景，暖调怀旧，前中后景层次，空气透视，生活使用痕迹，空镜无人",
		PropLook:   "手绘赛璐璐静物，材质用平涂+少量高光表达，不要摄影质感",
	},
	"2d-guofeng": {
		ID:         "2d-guofeng",
		Title:      "国风二次元新国潮",
		Gene:       "国风二次元新国潮，古风仙侠美学，日式动画渲染，赛璐璐平涂结合数字光影，东方留白与诗意构图",
		VideoTag:   "国风二次元动画，赛璐璐平涂，新国潮东方美学，电影风格，色彩鲜明，细腻笔触",
		Forbid:     "严禁真人写实、现代西装牛仔裤、手机电脑、霓虹赛博、3D游戏引擎质感",
		SkinF:      "冷白皮，赛璐璐平涂，皮肤通透发光，细腻线条",
		SkinM:      "健康冷调肤色，赛璐璐平涂，面容清冽",
		BodyF:      "默认 160-170cm，6-7 头身，削肩修长，身姿典雅",
		BodyM:      "默认 175-185cm，6.5-7.5 头身，肩线利落，身姿挺拔",
		HairF:      "墨黑长发，及腰，细腻发丝，自然散发或简单束发，基础态无发饰",
		HairM:      "墨黑长发或束发，发丝清晰，基础态无发冠",
		Clothes:    "默认古装：女素色长裙、男素色长衫；侠客/侍卫/公子按身份写对应形制。基础色，少花纹。禁止现代服装。",
		Background: "月白纯色背景",
		Light:      "均匀柔光，暖中带冷，无硬阴影，无冷色顶光",
		FaceRender: "国风二次元造型清晰，细腻线条，光影层次丰富",
		World:      "东方古代/仙侠世界，留白与诗意",
		SceneLook:  "古建庭院山水街市，青绿朱红靛蓝金黄传统色，东方留白，空镜头无人",
		PropLook:   "古风器物，木材漆器金属玉石质感，形制可读，无现代工业感",
	},
	"2d-flat": {
		ID:         "2d-flat",
		Title:      "2D扁平风",
		Gene:       "2D扁平矢量插画，纯色色块，无阴影无渐变，简洁线条，明快现代简约",
		VideoTag:   "2D扁平风格，几何造型，纯色色块，无阴影，简洁线条，现代简约",
		Forbid:     "严禁写实皮肤毛孔、体积光、厚涂、3D、复杂纹理、渐变阴影",
		SkinF:      "纯色肤色色块，无高光无毛孔",
		SkinM:      "纯色健康肤色色块，无体积阴影",
		BodyF:      "默认 160-168cm，简化几何比例，6.5-7.5 头身",
		BodyM:      "默认 175-182cm，简化几何比例，7-8 头身",
		HairF:      "大色块发型，边缘干净，无发丝根根分明",
		HairM:      "大色块短发或中发，边缘干净",
		Clothes:    "由身份决定的现代简约服装，大色块，无复杂花纹。禁止内衣打底。",
		Background: "纯净浅灰或纯白背景",
		Light:      "无光影，靠色块对比分层",
		FaceRender: "简洁五官色块，轮廓清晰",
		World:      "现代简约插画世界",
		SceneLook:  "扁平色块场景，前中后景用色块分层，无透视阴影，空镜无人",
		PropLook:   "扁平图标化静物，轮廓闭合，纯色填充",
	},
	"3d-cute": {
		ID:         "3d-cute",
		Title:      "3D可爱潮流",
		Gene:       "3D可爱潮流动画渲染，轮廓清晰，材质细腻，温暖明亮，治愈电影感，赛璐珞质感，适度卡通比例",
		VideoTag:   "3D动画渲染，赛璐珞质感，电影级光影，温暖色调，高细节材质，清晰轮廓线",
		Forbid:     "严禁真人皮肤毛孔、恐怖谷、泥塑脏污、过暗丧系",
		SkinF:      "柔光肌，皮肤通透发光，细腻卡通材质",
		SkinM:      "清爽卡通肤色，健康光泽",
		BodyF:      "默认 155-165cm，6-7 头身，略偏可爱比例",
		BodyM:      "默认 170-180cm，6.5-7.5 头身，利落但不写实健美",
		HairF:      "发丝根根分明的卡通长发，无发饰",
		HairM:      "发丝根根分明的卡通短发或中发，无发冠",
		Clothes:    "由身份决定的都市休闲/校服/职业便装，暖色调，无复杂花纹。禁止内衣打底。",
		Background: "纯净中性灰背景",
		Light:      "电影级柔和打光，均匀柔光，无硬阴影",
		FaceRender: "高细节卡通材质，面容细腻，发丝细腻",
		World:      "卡通都市，明快治愈",
		SceneLook:  "3D卡通场景，温暖明亮，材质清晰，空镜无人",
		PropLook:   "3D卡通静物，圆润边角，材质可读",
	},
	"3d-guofeng": {
		ID:         "3d-guofeng",
		Title:      "国风3D",
		Gene:       "国风3D，高精度建模与PBR材质，青绿朱红靛蓝金黄传统色盘，电影级体积光与东方意境",
		VideoTag:   "国风3D渲染，PBR材质，体积光，东方美学，典雅大气，电影风格",
		Forbid:     "严禁现代都市、西装球鞋、霓虹赛博、二次元大眼贴纸感",
		SkinF:      "细腻PBR皮肤，冷白带血色，体积光下通透",
		SkinM:      "健康古风肤色，PBR质感",
		BodyF:      "默认 160-170cm，6.5-7.5 头身，修长典雅",
		BodyM:      "默认 175-185cm，7-8 头身，挺拔英气",
		HairF:      "墨黑长发，PBR发丝，基础态无珠翠凤冠",
		HairM:      "墨黑束发或长发，PBR发丝，基础态无发冠",
		Clothes:    "古装形制：女素色长裙、男长衫或劲装，按身份可写侠客/官服。传统色，少堆砌饰品。",
		Background: "月白或浅青纯色背景",
		Light:      "电影级体积光，均匀柔和，无霓虹",
		FaceRender: "高精度面容与发丝，东方五官，PBR皮肤",
		World:      "东方古代建筑与自然意境",
		SceneLook:  "国风3D空间，传统色盘，体积光，空镜无人",
		PropLook:   "PBR古风器物，金属木漆玉石质感清晰",
	},
	"3d-clay": {
		ID:         "3d-clay",
		Title:      "定格黏土",
		Gene:       "定格黏土动画质感，黏土肌理与手指压痕可见，温暖柔和怀旧治愈，手工模型光影",
		VideoTag:   "定格动画黏土风格，黏土肌理，手指压痕，暖色调，柔和浅景深，奇幻3D卡通",
		Forbid:     "严禁真人皮肤、光滑塑料CG、锐利金属高光、恐怖裂缝",
		SkinF:      "哑光黏土肤色，可见轻微捏塑痕迹，圆润无尖锐棱角",
		SkinM:      "哑光黏土健康肤色，手工肌理",
		BodyF:      "默认 150-160cm 的黏土比例，偏圆润可爱",
		BodyM:      "默认 165-175cm 的黏土比例，圆润结实",
		HairF:      "黏土或羊毛感头发，块面清晰，无发饰",
		HairM:      "黏土短发块面，无发冠",
		Clothes:    "由身份决定的黏土服装，布料也是黏土/布偶质感。禁止写实面料摄影。",
		Background: "暖调纯色或浅木桌背景，保持干净",
		Light:      "定格摄影柔光，暖调，软阴影",
		FaceRender: "黏土五官圆润，表情克制，手工痕迹可见",
		World:      "手工模型世界，治愈怀旧",
		SceneLook:  "黏土场景，微缩模型，可见手工材质，空镜无人",
		PropLook:   "黏土或布偶静物，手指压痕，哑光",
	},
	"real-ancient": {
		ID:         "real-ancient",
		Title:      "真人古风",
		Gene:       "真人古风写实影视质感，古代宅邸宫殿服饰，35mm胶片颗粒，冷中带暖，杜绝现代元素",
		VideoTag:   "古风写实摄影，电影风格，强对比度，极致细节",
		Forbid:     "严禁手机、眼镜、拉链、运动鞋、二次元大眼、3D卡通",
		SkinF:      "自然肤色，毛孔微可见，冷白但有血色，健康光泽",
		SkinM:      "自然或小麦肤色，毛孔清晰，清爽质感",
		BodyF:      "默认 160-170cm，7-8 头身，真实人体比例",
		BodyM:      "默认 175-185cm，7.5-8.5 头身，真实人体比例",
		HairF:      "自然黑发或深棕长发，发丝根根分明，基础态无华丽头面",
		HairM:      "自然黑发束发或长发，发丝真实，基础态无发冠",
		Clothes:    "古装形制按身份：女襦裙/长裙，男长袍/劲装。杜绝现代剪裁。禁止内衣出镜。",
		Background: "纯净中性灰背景",
		Light:      "自然光照，物理光影，均匀柔光，胶片颗粒",
		FaceRender: "毛孔级面容，发丝真实，皮肤真实质感",
		World:      "古代中国宅邸宫殿市井，无任何现代物",
		SceneLook:  "古风实拍空间，自然光，胶片，空镜无人",
		PropLook:   "古代器物实拍静物，材质真实，无现代工业件",
	},
	"real-urban": {
		ID:         "real-urban",
		Title:      "真人都市",
		Gene:       "真人现代都市写实，电影胶片颗粒，自然光影，低对比度调色，纯实拍质感",
		VideoTag:   "真人都市电影摄影，真人实拍质感，当代中国都市，电影级色彩科学，自然光与实用光源调度，浅景深，手持呼吸感或稳定器流动，电影颗粒质感，视频动态优化，非CG非渲染",
		Forbid:     "严禁二次元大眼、赛璐璐、3D卡通、过度磨皮、网红美颜脸",
		SkinF:      "自然肤色，毛孔微可见，可偏白/偏黄，健康光泽，非哑光非油光",
		SkinM:      "自然或小麦肤色，毛孔清晰，可有细微瑕疵与胡青",
		BodyF:      "默认 158-170cm，7-8 头身，真实都市女性体态",
		BodyM:      "默认 172-185cm，7.5-8.5 头身，真实都市男性体态；拳手/格斗者可写训练痕迹与肩背肌理，但保持真人比例",
		HairF:      "自然发色（黑/深棕），及肩或更长，发丝根根分明，无夸张染发",
		HairM:      "自然黑或深棕短发/中发，发丝真实",
		Clothes:    "由身份决定日常默认态：学生校服、上班族衬衫西装、休闲卫衣牛仔裤、特殊职业工装（拳手→训练背心或连帽衫等）。低饱和中性色，无复杂花纹。禁止内衣打底。",
		Background: "纯净中性灰 #E8E8E8 背景",
		Light:      "均匀柔光，前方主光+双侧补光，电影胶片，无硬阴影",
		FaceRender: "毛孔级面容，发丝根根分明，皮肤真实质感",
		World:      "当代都市现实世界",
		SceneLook:  "实拍都市空间，自然光或室内灯光，生活痕迹，空镜无人",
		PropLook:   "现代静物实拍，材质真实，品牌感克制",
	},
}

func artSystemPrompt(assetType string, style visualStyle, derivative bool) string {
	var prompt string
	switch normalizeAssetType(assetType) {
	case "scene":
		if derivative {
			prompt = sceneDerivativeArtSystem(style)
		} else {
			prompt = sceneArtSystem(style)
		}
	case "prop":
		prompt = propArtSystem(style)
	default:
		if derivative {
			prompt = characterDerivativeArtSystem(style)
		} else {
			prompt = characterArtSystem(style)
		}
	}
	return services.ApplyDramaSkillGuidance(prompt, "assets", "image-prompts")
}

func characterArtSystem(s visualStyle) string {
	return fmt.Sprintf(`你是角色设定图提示词专家。任务：根据角色名称与描述，输出一条可直接用于文生图的「角色四视图设定图」提示词。

必须严格、完整遵循下方约束，并按提示词模板把所有花括号替换成具体描写。
仅输出提示词正文，不得附加解释、标题、Markdown、JSON 或代码块。

# 风格
画风：%s
%s
%s

# 原则
1. 面容即灵魂：五官由角色描述自然推导，人物之间必须能一眼区分。不要用「英俊/漂亮/气质好」这种空词，要写眼型、眉形、鼻梁、唇形、疤痕、肤质等可画细节。
2. 描述里出现的记忆点（疤痕、心口旧伤的体态、拳手鼻梁、冷笑纹、陪侍妆感等）必须写进提示词；没有写明的，根据身份年龄性格合理推断并略作夸张，但不得编造与剧情冲突的奇装异服。
3. 基础着装由身份/职业决定日常默认态，不是剧情里某一场的临时造型。
4. 四视图的面容、体型、发型、服装必须一致。
5. 中性微表情，符合气质；禁止夸张表情和动态姿势。
6. 基础态声明无发饰、无夸张配饰。剧情道具（冠军奖牌、奖杯、武器、手机）禁止画成常戴项链或握在设定图手里——那些要单独出道具图。脖子保持干净，除非角色描述写明常年佩戴的身份信物（且不是刚赢来的奖牌）。

# 分性别默认（可被角色描述覆盖）
女性：%s；体型 %s；发型 %s
男性：%s；体型 %s；发型 %s
着装：%s

# 画面规格（必须全部写入提示词）
同一画面从左至右并排四视图：人像特写 + 正视图 + 侧视图 + 后视图。
- 人像特写：正面平视，从头顶到锁骨完整，不裁切头顶，面部占特写区域 60%% 以上
- 正视图：正面 0° 全身立像，面对镜头，双臂自然下垂
- 侧视图：右侧 90° 全身，轮廓清晰
- 后视图：180°，后脑/背/发尾/鞋履清晰
全身必须从头顶到脚底完整入画，严禁裁切。
背景：%s。站姿自然，双脚平行微分。光线：%s。
character design sheet, character turnaround, portrait closeup, front view, side view, back view, full body head to toe, head to collarbone complete。
图中不要有任何文字、尺寸标注、箭头、色卡、其他人物。

# 提示词模板（按此语序写成一段或数行，花括号必须全部落实）
{性别}角色四视图设定图，{画风锚定词}，强对比度，极致细节，
character design sheet，character turnaround，
{五官记忆点}，{气质关键词}，{素颜或自然状态}，
{肤色与肤感}，{身高cm}，{头身比}，{身材}，{体态}，
{发色发长发型}，{服装上装+下装+鞋，写清颜色与材质}，
同一画面左至右并排：人像特写+正视图+侧视图+后视图，
人像特写从头顶到锁骨完整展示，不裁切头顶，
全身立像从头顶到脚底完整展示，不裁切头顶和脚部，
自然站立，{背景}，{光线}，四视图一致性，%s
图中不要有任何文字

# 严禁
内衣/暴露/性化；复杂场景背景；夸张表情动作；裁切头顶或脚；只写剧情身份而不写外形；输出 JSON。
`, s.Title+"。"+s.Gene, s.Forbid, s.World, s.SkinF, s.BodyF, s.HairF, s.SkinM, s.BodyM, s.HairM, s.Clothes, s.Background, s.Light, s.FaceRender)
}

func sceneArtSystem(s visualStyle) string {
	return fmt.Sprintf(`你是场景概念图提示词专家。根据场景名称与描述，输出一条可直接用于文生图的空镜场景提示词。

仅输出提示词正文，不要 JSON、解释或 Markdown。

画风：%s
%s
场景质感：%s
世界：%s

必须包含：空间类型（室内/室外）+ 时代风格 + 前中后景具体物件 + 光线时间 + 氛围情绪。
单画面主视图，不是四宫格。严禁出现任何人物、人影、人体轮廓。
材质要有使用痕迹，不要全新无瑕的塑料感。图中不要文字。

模板语序：
{画风}场景主视图概念图，environment concept art，no people，
{室内或室外}，{场景类型}，{时间天气}，
前景：{物件}，中景：{主体空间}，后景：{纵深}，
{光线}，{色调}，{材质}，空气透视，
单画面构图，画面中无任何人物，图中不要有任何文字
`, s.Title+"。"+s.Gene, s.Forbid, s.SceneLook, s.World)
}

func propArtSystem(s visualStyle) string {
	return fmt.Sprintf(`你是道具设定图提示词专家。根据道具名称与描述，输出一条可直接用于文生图的道具四宫格设定提示词。

仅输出提示词正文，不要 JSON、解释或 Markdown。

画风：%s
%s
道具质感：%s

纯静物陈列：画面只能出现道具本身，严禁人物、手、肢体、佩戴或握持状态。
同一画面四宫格：正面、侧面、背面、细节特写。背景纯净中性灰，均匀柔光。
必须写清材质、颜色、尺度感、标志性细节与磨损。图中不要文字。

模板语序：
{画风}道具设定图，prop design sheet，no people，
{道具}，{材质颜色工艺}，{标志细节}，
纯道具静物独立陈列，无人持有，
四宫格：正面+侧面+背面+细节特写，中性灰背景，均匀柔光
图中不要有任何文字
`, s.Title+"。"+s.Gene, s.Forbid, s.PropLook)
}

func characterDerivativeArtSystem(s visualStyle) string {
	return fmt.Sprintf(`你是角色衍生服化提示词专家。在父资产底模上叠加换装/妆造/发型，输出一条可直接用于文生图（参考底模图）的四视图提示词。

仅输出提示词正文，不要 JSON、表格、解释。

画风：%s
%s

# 叠加原则
1. 面容、体型、头身比、站姿与底模完全一致，禁止面容偏移和任何动作。
2. 只叠加服化妆造：妆容、发型发饰、中衣、主服、配饰、鞋履。
3. 禁止场景/天气/室内外；禁止手持道具（手机、伞、杯）；禁止行走回眸举手。
4. 按身份与这场状态差异化穿搭，不要套所有人同一套衣服。
5. 所有衍生都要有妆造（至少基础妆/伪素颜），强度看面部线索，不能仅因换装就画浓妆。
6. 必须写清鞋履款式与材质。
7. 配饰与父图一致：父图脖子没有奖牌/项链就不要加；禁止把奖杯、奖牌画成项链。赤膊/战损只改衣服与伤势，不要发明新配饰。

# 分层（全部写入提示词）
【L1·妆容】基础妆/轻妆/正式妆 + 风格
【L2·发型】造型 + 发饰（可无）
【L3+L4·服饰】颜色款式材质，相对底模改了什么
【L5·配饰】与父图一致，无则写「无配饰」
【L6·鞋履】鞋型材质颜色

# 画面规格
同一画面左至右：人像特写+正视图+侧视图+后视图，灰底，自然站立，均匀柔光。
保持基础形象面容不变。character design sheet，character turnaround。图中不要文字。

模板：
以角色基础形象图为底图叠加服化妆造，{性别}角色四视图设定图，{画风}，
character design sheet，character turnaround，保持基础形象面容不变，{气质}，
【L1·妆容】…，【L2·发型】…，【L3+L4·服饰】…，【L5·配饰】…，【L6·鞋履】…，
同一画面左至右并排：人像特写+正视图+侧视图+后视图，自然站立，%s，%s，四视图一致性
图中不要有任何文字
`, s.Title+"。"+s.Gene, s.Forbid, s.Background, s.Light)
}

func sceneDerivativeArtSystem(s visualStyle) string {
	return fmt.Sprintf(`你是场景时段衍生提示词专家。在父场景主视图上只改时间/光照氛围，输出一条空镜场景提示词。

仅输出提示词正文，不要 JSON、解释。

画风：%s
%s
场景质感：%s

必须保持建筑结构、布局、主要物件与父场景一致。只改变时段带来的天空、灯光、色温、阴影。
单画面主视图，严禁人物。图中不要文字。

模板：
同一场景时段变体概念图，environment concept art，no people，
保持原空间结构与物件布局，{目标时段}，{光线色调}，{氛围}，
前中后景与父场景一致，空气透视，画面中无任何人物
图中不要有任何文字
`, s.Title+"。"+s.Gene, s.Forbid, s.SceneLook)
}

func visualUserPrompt(item AssetItem, project models.Project) string {
	label := "角色"
	switch normalizeAssetType(item.Type) {
	case "scene":
		label = "场景"
	case "prop":
		label = "道具"
	}
	var b strings.Builder
	b.WriteString("**基础参数：**\n")
	if item.IsDerivative || item.ParentID > 0 {
		b.WriteString("这是衍生资产，必须保持父资产面容/体型/空间结构不变，只叠加状态差异。\n")
		if strings.TrimSpace(item.ParentName) != "" {
			b.WriteString("- 父资产名称：")
			b.WriteString(strings.TrimSpace(item.ParentName))
			b.WriteString("\n")
		}
		if strings.TrimSpace(item.ParentDescription) != "" {
			b.WriteString("- 父资产描述：")
			b.WriteString(strings.TrimSpace(item.ParentDescription))
			b.WriteString("\n")
		}
	}
	b.WriteString("**")
	b.WriteString(label)
	b.WriteString("设定：**\n")
	b.WriteString("- ")
	b.WriteString(label)
	b.WriteString("名称：")
	b.WriteString(strings.TrimSpace(item.Name))
	b.WriteString("\n- ")
	b.WriteString(label)
	b.WriteString("描述：")
	desc := strings.TrimSpace(item.Description)
	if desc == "" {
		desc = strings.TrimSpace(item.Prompt)
	}
	if desc == "" {
		desc = "请根据名称与项目简介合理推断可绘制的外形，并写清记忆点。"
	}
	b.WriteString(desc)
	b.WriteString("\n")
	if ctx := visualProjectContext(project); ctx != "" {
		b.WriteString("\n项目信息：\n")
		b.WriteString(ctx)
	}
	return b.String()
}

func visualProjectContext(project models.Project) string {
	var b strings.Builder
	if g := strings.TrimSpace(project.Genre); g != "" {
		b.WriteString("题材：")
		b.WriteString(g)
		b.WriteString("\n")
	}
	if s := strings.TrimSpace(project.Synopsis); s != "" {
		b.WriteString("故事简介：")
		b.WriteString(clipRunes(s, 500))
		b.WriteString("\n")
	}
	return b.String()
}

func parsePromptOutput(raw string) string {
	s := strings.TrimSpace(stripThink(stripFence(raw)))
	s = strings.TrimPrefix(s, "提示词：")
	s = strings.TrimPrefix(s, "提示词:")
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	var boxed struct {
		Prompt string `json:"prompt"`
		Assets []struct {
			Prompt string `json:"prompt"`
		} `json:"assets"`
	}
	if unmarshalObject(s, &boxed) == nil {
		if p := strings.TrimSpace(boxed.Prompt); p != "" {
			return p
		}
		if len(boxed.Assets) == 1 {
			if p := strings.TrimSpace(boxed.Assets[0].Prompt); p != "" {
				return p
			}
		}
	}
	return s
}

func stripThink(raw string) string {
	s := raw
	for {
		start := strings.Index(s, "<think>")
		end := strings.Index(s, "</think>")
		if start < 0 || end < 0 || end <= start {
			break
		}
		s = s[:start] + s[end+len("</think>"):]
	}
	return strings.TrimSpace(s)
}
