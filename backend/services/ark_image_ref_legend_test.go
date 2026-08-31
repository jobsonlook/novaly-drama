package services

import (
	"strings"
	"testing"
)

func TestBuildResourceImageRefLegendMixedRefs(t *testing.T) {
	got := BuildResourceImageRefLegend([]ImageRefMeta{
		{Label: "地下拳赛冠军奖牌", Kind: "prop"},
		{Label: "韩铮", Kind: "character"},
	})
	if !strings.Contains(got, "图1为地下拳赛冠军奖牌（道具）") {
		t.Fatalf("missing prop mapping: %s", got)
	}
	if !strings.Contains(got, "图2为韩铮（角色）") {
		t.Fatalf("missing character mapping: %s", got)
	}
	if !strings.Contains(got, ResourceImageRefFusionConstraint) {
		t.Fatalf("mixed refs should include fusion rule: %s", got)
	}
}

func TestPrependImageRefLegendPutsLegendFirst(t *testing.T) {
	legend := BuildResourceImageRefLegend([]ImageRefMeta{
		{Label: "奖牌", Kind: "prop"},
		{Label: "韩铮", Kind: "character"},
	})
	got := PrependImageRefLegend("原定妆照要求：蓝衬衫", legend)
	if !strings.HasPrefix(got, "参考图：") {
		t.Fatalf("legend should lead: %s", got)
	}
	if !strings.Contains(got, "原定妆照要求：蓝衬衫") {
		t.Fatalf("original prompt missing: %s", got)
	}
	if PrependImageRefLegend(got, legend) != got {
		t.Fatal("should not prepend twice")
	}
}

func TestStripImageRefLegendKeepsReverseLegend(t *testing.T) {
	poison := "【空间】包厢站位图：参考图：图1为阿彪举杯敬韩铮 · 站位图 · 候选1（场景），图2为私人会所包厢·俯视全景·候选1（场景）。\n按图号引用上方参考图，不要弄混。\n生成图1的反打镜头"
	got := StripImageRefLegend(poison)
	if strings.Contains(got, "图1为阿彪") || strings.Contains(got, "俯视全景") {
		t.Fatalf("should strip resource-name legend: %s", got)
	}
	kept := StripImageRefLegend(SceneReverseRefLegend + "\n\n成片按机位B")
	if !strings.Contains(kept, "反打镜头线稿") || !strings.Contains(kept, "成片按机位B") {
		t.Fatalf("should keep reverse legend: %s", kept)
	}
}
