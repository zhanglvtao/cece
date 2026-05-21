package theme

import (
	"testing"
)

func TestDefaultPaletteAllColorsNonNil(t *testing.T) {
	p := DefaultPalette()
	checks := []struct {
		name  string
		color interface{}
	}{
		{"Primary", p.Primary},
		{"Secondary", p.Secondary},
		{"Accent", p.Accent},
		{"Keyword", p.Keyword},
		{"FgBase", p.FgBase},
		{"FgSubtle", p.FgSubtle},
		{"FgMuted", p.FgMuted},
		{"FgFaint", p.FgFaint},
		{"BgBase", p.BgBase},
		{"BgSubtle", p.BgSubtle},
		{"BgFaint", p.BgFaint},
		{"BgHighlight", p.BgHighlight},
		{"OnPrimary", p.OnPrimary},
		{"Separator", p.Separator},
		{"Destructive", p.Destructive},
		{"DestructiveMuted", p.DestructiveMuted},
		{"Success", p.Success},
		{"SuccessMuted", p.SuccessMuted},
		{"SuccessFaint", p.SuccessFaint},
		{"Warning", p.Warning},
		{"WarningMuted", p.WarningMuted},
		{"Info", p.Info},
		{"InfoMuted", p.InfoMuted},
		{"InfoFaint", p.InfoFaint},
		{"Busy", p.Busy},
	}
	for _, c := range checks {
		if c.color == nil {
			t.Errorf("DefaultPalette().%s is nil", c.name)
		}
	}
}

func TestHexColorRoundTrip(t *testing.T) {
	p := DefaultPalette()
	got := HexColor(p.Primary)
	if len(got) != 7 || got[0] != '#' {
		t.Errorf("HexColor(Primary) = %q, want #RRGGBB format", got)
	}
}
