package crew

import "testing"

func TestEstimateStoryboardCountByScenesAndDialogue(t *testing.T) {
	script := `**出场人物：韩铮，阿彪**

**1-1 更衣室 [内] [夜]**
△ 韩铮坐在木凳边，双手伸进冰水桶。
**韩铮**：（冷）今晚的对手已经把场子买通了。
△ 阿彪把衬衫递过来。
**阿彪**：先把扣子扣上。

**1-2 走廊 [内] [夜]**
△ 两人走向拳台通道。
**阿彪**：杀手甲就在门口。
△ 韩铮扣好最后一颗扣子。

**1-3 拳台入口 [内] [夜]**
△ 闪回：地下拳王奖杯被举起。
△ 回到现在，韩铮抬头。
`
	n := EstimateStoryboardCountForPace(script, StoryboardPacePacked)
	if n < 3 || n > 8 {
		t.Fatalf("expected a stable mid range, got %d", n)
	}
	lo, hi := StoryboardCountRangeForPace(n, StoryboardPacePacked)
	if lo > n || hi < n || hi-lo > 2 {
		t.Fatalf("range %d-%d around %d", lo, hi, n)
	}
}

func TestEstimateFineIsDenserThanPacked(t *testing.T) {
	script := `**1-1 更衣室 [内] [夜]**
△ 韩铮坐在木凳边。
**韩铮**：（冷）今晚的对手已经把场子买通了。
△ 阿彪把衬衫递过来。
**阿彪**：先把扣子扣上。
`
	packed := EstimateStoryboardCountForPace(script, StoryboardPacePacked)
	fine := EstimateStoryboardCountForPace(script, StoryboardPaceFine)
	if fine <= packed {
		t.Fatalf("fine should cut denser than packed: fine=%d packed=%d", fine, packed)
	}
}

func TestEstimateSameScriptIsStable(t *testing.T) {
	script := "**1-1 更衣室 [内] [夜]**\n△ 韩铮浸冰水。\n**韩铮**：把场子买通了。\n**1-2 拳台 [内] [夜]**\n△ 韩铮走上拳台。"
	a := EstimateStoryboardCount(script)
	b := EstimateStoryboardCount(script)
	if a != b {
		t.Fatalf("estimator must be deterministic: %d vs %d", a, b)
	}
}

func TestEstimateEmptyIsTwo(t *testing.T) {
	if EstimateStoryboardCount("") != 2 {
		t.Fatal("empty should be 2")
	}
}
