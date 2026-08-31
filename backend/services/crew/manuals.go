package crew

import "strings"

func StylePrompt(key string) string {
	switch strings.TrimSpace(key) {
	case "2d-90s-anime":
		return "90年代日式动画风格，手绘平涂，清晰流畅线条，柔和暖调，电影感光影层次，怀旧治愈，赛璐璐上色"
	case "2d-guofeng":
		return "国风二次元新国潮，古风仙侠美学，日式动画渲染，赛璐璐平涂结合数字光影，东方留白与诗意构图"
	case "2d-flat":
		return "2D扁平风，纯色色块，无阴影无渐变，简洁线条，明快现代简约插画"
	case "3d-cute":
		return "3D可爱潮流动画渲染，轮廓清晰，材质细腻，温暖明亮，治愈电影感"
	case "3d-guofeng":
		return "国风3D，高精度建模与PBR材质，青绿朱红靛蓝金黄传统色盘，电影级体积光与东方意境"
	case "3d-clay":
		return "定格黏土动画质感，黏土肌理与手指压痕可见，温暖柔和怀旧治愈，手工模型光影"
	case "real-ancient":
		return "真人古风写实影视质感，古代宅邸宫殿服饰，35mm胶片颗粒，冷中带暖，杜绝现代元素"
	case "real-urban":
		return "真人现代都市写实，电影胶片颗粒，自然光影，低对比度调色，纯实拍质感"
	default:
		return ""
	}
}

func DirectorBrief(key string) string {
	switch strings.TrimSpace(key) {
	case "comedy":
		return "喜剧搞笑：用预期违背制造笑点，节奏优先于段子；角色性格碰撞出荒谬，笑中带泪，铺垫→抖包袱循环。"
	case "coming-of-age":
		return "青春成长：抓住身份迷茫与第一次选择，用细节时代感与关系对照完成成长弧光，避免说教。"
	case "family":
		return "家庭温情：日常细节承载情感，克制煽情；代际冲突用理解和错过推进，结尾留有温度。"
	case "historical":
		return "历史史诗：格局开阔、仪式感强，个人命运与时代洪流交织；场面调度大气，情绪庄重。"
	case "horror":
		return "恐怖灵异：信息克制、空间压迫、声音先行；怕在未见之时，少直给怪物，多给规则与代价。"
	case "mystery":
		return "悬疑推理：线索公平摆放，每场有新信息或新疑问；反转需前面埋点，禁止无中生有。"
	case "xianxia":
		return "仙侠奇幻：境界与代价并存，法术可视化但服务人物关系；奇观与情感并重，避免堆砌设定。"
	case "workplace":
		return "都市职场：利益与体面拉扯，潜台词多于直说；空间用办公室/应酬场制造权力关系。"
	case "romance":
		return "甜宠言情：心动来自具体互动而非旁白；拉扯有理由，亲密戏克制而有记忆点。"
	case "scifi":
		return "科幻末日：先立规则再破规则；废土/科技奇观服务生存选择与人性，不空转设定。"
	case "action":
		return "热血动作：动作拆成准备-爆发-余韵；镜头跟情绪走，打戏要看清因果与代价。"
	case "psychological":
		return "心理剧：内心外化为行为与空间隐喻，节奏偏慢但信息密；少解释，多让观众自己感到不安。"
	default:
		return ""
	}
}
