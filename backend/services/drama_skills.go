package services

import "strings"

// DramaSkillGuidance adapts the installed drama-skills suite to Novaly's
// database-backed pipeline. The original suite's filesystem lifecycle,
// dashboard and publishing transactions intentionally stay out of prompts;
// Novaly already owns those concerns. Each agent receives only its stage's
// creative contract so the guidance remains focused and token-bounded.
var dramaSkillGuidance = map[string]string{
	"develop": `【内置短剧技能 · 故事开发】
- 先锁定本集对观众的承诺、人物欲望、阻力、转折与结尾钩子，再安排镜头或资产。
- 每个节拍必须改变信息、关系、选择或风险，禁止只靠情绪标签推进。
- 严格区分角色知道的信息、观众知道的信息与本拍新揭示的信息；不得提前泄露后续答案。
- 保留原剧本的因果链和作者意图，不用通用爽点模板覆盖具体人物逻辑。`,
	"write": `【内置短剧技能 · 分集写作】
- 台词、动作和场面调度都必须服务当前人物目标，并由上一拍触发、向下一拍留下可回应结果。
- 修改现成剧本时保留原文事实、说话人和作者声音；不得静默改变语义。
- 用可拍行为替代心理解释与创作分析标签，避免模板化反应、空停顿和泛化情绪。
- 场次边界由地点、时间、目标或冲突变化决定，不为凑节奏任意切断因果。`,
	"assets": `【内置短剧技能 · 资产与连续性】
- 先判断复用基础资产、建立状态变体还是新增资产；只有可观察差异才建立变体。
- 身份、造型状态、持物状态、地点与时间状态分开记录，禁止把剧情道具烘焙进角色基础外观。
- 同一角色跨镜保持面容、体型、服装阶段、配饰和声音方向连续；状态变化必须有明确发生点。
- 资产描述写稳定可复用的可见事实，不把单镜动作、情绪或镜头语言写成永久设定。`,
	"image-prompts": `【内置短剧技能 · 图片提示词】
- 角色图只负责身份与稳定外观；场景图只负责地理、结构、材质和光线；道具图只负责物件身份与状态。
- 参考图职责不可越权：风格参考不接管人物身份，角色参考不接管场景构图，场景空镜不得带人。
- 状态变体只写相对父资产的可观察变化，并明确必须保持不变的身份、体型或空间结构。
- 提示词使用可绘制事实，避免剧情概述、抽象性格、备选项和互相冲突的要求。`,
	"storyboard": `【内置短剧技能 · 分镜】
- 每镜先明确本镜承载的原文事实、观众新获得的信息和戏剧转向，再决定观看位置与景别。
- 镜头必须写清主体、动作对象、视线交接、站位、持物和起止状态；禁止让模型猜“对方/听者”是谁。
- 口播对白拍：说话人必须在画面里开口；禁止台词归属 A 而镜头只写 B、C。听者反应可入画，但主体是说话人。
- 对白拍具名角色宜少，避免一镜堆多人导致视频模型对错口型；换说话人就切到该说话人。
- 不跨越已接受的场次或镜头边界，不提前消费后镜动作与揭示；首尾状态必须可与相邻镜衔接。
- 景别与运镜应改变信息或观看关系，禁止用无动机推拉、重复反应和模板化正反打填时长。`,
	"video-prompts": `【内置短剧技能 · 视频提示词】
- 只描述当前已接受镜头边界内的运动，不新增前因、后果、角色、台词或镜头。
- 明确每个主体的初始位置、动作过程、注意力交接、结束姿态，以及摄影机起点、运动和终点。
- 多人表演逐人写动作与视线对象，维持左右、前后、朝向、持物和服装连续。
- 对白、环境声和动作声分别标明来源；无台词镜头不得凭空配音，提示词不得把对白变成字幕。`,
	"review": `【内置短剧技能 · 独立审查】
- 只发布有具体镜头证据的问题、损失和可验证修订要求；不得在审查阶段直接重写来源。
- 优先检查原文落实、因果、观众信息、资产职责、状态连续、视线对象、镜头边界与起止状态。
- 不把个人偏好当错误，不新增创作者未授权的风格或剧情要求；通过项不要列入问题。
- 复检只确认原问题是否消失，不借机扩张范围或制造新问题。`,
}

// ApplyDramaSkillGuidance appends one or more stage contracts to an existing
// prompt. Unknown or duplicate stages are ignored.
func ApplyDramaSkillGuidance(prompt string, stages ...string) string {
	out := strings.TrimSpace(prompt)
	seen := map[string]bool{}
	for _, stage := range stages {
		stage = strings.TrimSpace(stage)
		if stage == "" || seen[stage] {
			continue
		}
		guide := strings.TrimSpace(dramaSkillGuidance[stage])
		if guide == "" {
			continue
		}
		seen[stage] = true
		out += "\n\n" + guide
	}
	return strings.TrimSpace(out)
}
