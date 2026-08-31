package crew

import (
	"strings"
	"testing"

	"novaly/backend/models"
)

func TestVideoLookPackToonflowTags(t *testing.T) {
	cases := []struct {
		manual string
		want   string
		forbid string
	}{
		{"2d-90s-anime", "90年代日式动画，手绘赛璐璐，柔和暖调，电影风格，清晰线条，怀旧质感", "严禁真人写实"},
		{"2d-guofeng", "国风二次元动画，赛璐璐平涂，新国潮东方美学，电影风格，色彩鲜明，细腻笔触", "严禁真人写实"},
		{"2d-flat", "2D扁平风格，几何造型，纯色色块，无阴影，简洁线条，现代简约", "严禁写实皮肤毛孔"},
		{"3d-cute", "3D动画渲染，赛璐珞质感，电影级光影，温暖色调，高细节材质，清晰轮廓线", "严禁真人皮肤毛孔"},
		{"3d-guofeng", "国风3D渲染，PBR材质，体积光，东方美学，典雅大气，电影风格", "严禁现代都市"},
		{"3d-clay", "定格动画黏土风格，黏土肌理，手指压痕，暖色调，柔和浅景深，奇幻3D卡通", "严禁真人皮肤"},
		{"real-ancient", "古风写实摄影，电影风格，强对比度，极致细节", "严禁手机"},
		{"real-urban", "真人都市电影摄影，真人实拍质感，当代中国都市", "严禁二次元大眼"},
	}
	for _, tc := range cases {
		got := VideoLookPack(models.Project{VisualManual: tc.manual}, "")
		if !strings.Contains(got, tc.want) {
			t.Fatalf("%s missing tag %q:\n%s", tc.manual, tc.want, got)
		}
		if !strings.Contains(got, tc.forbid) {
			t.Fatalf("%s missing forbid %q:\n%s", tc.manual, tc.forbid, got)
		}
		if strings.Contains(got, "肤色") || strings.Contains(got, "发丝") {
			t.Fatalf("%s leaked character sheet wording:\n%s", tc.manual, got)
		}
	}
}

func TestVideoLookPackShotOverlayAndExtraStyle(t *testing.T) {
	got := VideoLookPack(models.Project{
		VisualManual: "real-urban",
		Style:        "冷调夜戏，霓虹少一点",
	}, "本镜偏手持呼吸感")
	for _, part := range []string{
		"真人都市电影摄影",
		"项目补充画风：冷调夜戏，霓虹少一点",
		"本镜补充：本镜偏手持呼吸感",
		"严禁二次元大眼",
	} {
		if !strings.Contains(got, part) {
			t.Fatalf("missing %q:\n%s", part, got)
		}
	}
}
