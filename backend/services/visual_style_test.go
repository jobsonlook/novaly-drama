package services

import "testing"

func TestUsesPhotorealPeople(t *testing.T) {
	tests := []struct {
		style string
		want  bool
	}{
		{"", true},
		{"国风真人写实电影", true},
		{"国风 3D 动漫，PBR 材质", false},
		{"写实比例的3D动画，禁止真人照片", false},
		{"二维手绘插画", false},
	}
	for _, tt := range tests {
		if got := UsesPhotorealPeople(tt.style); got != tt.want {
			t.Errorf("UsesPhotorealPeople(%q) = %v, want %v", tt.style, got, tt.want)
		}
	}
}
