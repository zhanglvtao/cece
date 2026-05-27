package anim

import (
	"image/color"

	"github.com/lucasb-eyer/go-colorful"
)

// BlendHCL interpolates between two colors in HCL space at ratio t.
// t=0 returns a, t=1 returns b. Produces perceptually uniform gradients.
func BlendHCL(a, b color.Color, t float64) color.Color {
	ca, _ := colorful.MakeColor(a)
	cb, _ := colorful.MakeColor(b)
	return ca.BlendHcl(cb, clamp01(t))
}

// BlendAlpha simulates alpha compositing by interpolating fg over bg.
// alpha=0 returns bg, alpha=1 returns fg.
func BlendAlpha(bg, fg color.Color, alpha float64) color.Color {
	cb, _ := colorful.MakeColor(bg)
	cf, _ := colorful.MakeColor(fg)
	return cb.BlendHcl(cf, clamp01(alpha))
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
