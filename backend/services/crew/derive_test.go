package crew

import "testing"

func TestIsIndependentSceneName(t *testing.T) {
	cases := []struct {
		name, parent string
		want         bool
	}{
		{"归元宗祖师殿", "归元宗祖师殿前", true},
		{"归墟断桥下", "归墟断桥", true},
		{"归墟龙心殿外", "归墟龙心殿", true},
		{"归元宗护山阵", "归元宗山门", true},
		{"夜景", "归元宗祖师殿前", false},
		{"黄昏", "归元宗山门", false},
		{"剑鸣之夜", "归元宗祖师殿前", false},
		{"清晨时分", "归元宗后山荒田", false},
	}
	for _, tc := range cases {
		if got := isIndependentSceneName(tc.name, tc.parent); got != tc.want {
			t.Fatalf("isIndependentSceneName(%q,%q)=%v, want %v", tc.name, tc.parent, got, tc.want)
		}
	}
}
