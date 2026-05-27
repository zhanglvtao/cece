package anim

import (
	"image/color"
	"math"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/lucasb-eyer/go-colorful"
)

// AnimState represents the animation state machine.
type AnimState uint8

const (
	AnimIdle   AnimState = iota // No animation, static border
	AnimBusy                    // Active, breathing border
	AnimFading                  // Finishing, fading to idle
)

// TickMsg drives the animation at 20fps.
type TickMsg struct{}

// Engine drives a breathing border animation with Idle/Busy/Fading states.
// Breathing interpolates from a desaturated (grayish) version of the base
// color to the fully saturated base color — same hue, just the chroma
// (saturation) oscillates, producing a "glow from gray" effect.
type Engine struct {
	state AnimState

	// Breathing oscillator.
	breathPeriod time.Duration
	breathStart  time.Time

	// Color endpoints in HCL space: desaturated gray ↔ vivid base.
	// Both share the same hue, so BlendHcl only changes chroma and lightness.
	from colorful.Color
	to   colorful.Color

	// Fade tracking for Busy → Idle transition.
	fadeStart    time.Time
	fadeDuration time.Duration
}

// NewEngine creates an animation engine that breathes from a desaturated
// version of the base color to the fully saturated base color.
func NewEngine(base color.Color) *Engine {
	c, _ := colorful.MakeColor(base)
	h, _, l := c.Hcl()
	// Desaturated start: same hue and lightness, minimal chroma (grayish-white look).
	desaturated := colorful.Hcl(h, 0.05, l)

	return &Engine{
		state:        AnimIdle,
		breathPeriod: 3 * time.Second,
		breathStart:  time.Now(),
		from:         desaturated,
		to:           c,
		fadeDuration: 800 * time.Millisecond,
	}
}

// TickCmd returns a tea.Cmd that sends TickMsg at 20fps.
func TickCmd() tea.Cmd {
	return tea.Tick(50*time.Millisecond, func(time.Time) tea.Msg {
		return TickMsg{}
	})
}

// OnBusy transitions to the Busy state and resets the breath oscillator.
func (e *Engine) OnBusy(now time.Time) {
	e.state = AnimBusy
	e.breathStart = now
}

// OnFinishTurn transitions to the Fading state.
func (e *Engine) OnFinishTurn(now time.Time) {
	e.state = AnimFading
	e.fadeStart = now
}

// State returns the current animation state.
func (e *Engine) State() AnimState {
	return e.state
}

// Advance moves the animation forward one frame.
// Returns true if the animation is still active and needs more ticks.
func (e *Engine) Advance(now time.Time) bool {
	switch e.state {
	case AnimBusy:
		return true
	case AnimFading:
		if now.Sub(e.fadeStart) >= e.fadeDuration {
			e.state = AnimIdle
			return false
		}
		return true
	case AnimIdle:
		return false
	}
	return false
}

// ShouldTick returns whether the engine needs more frames.
func (e *Engine) ShouldTick() bool {
	return e.state == AnimBusy || e.state == AnimFading
}

// BorderColor returns the current animated border color.
// During Busy: breathes from desaturated gray to vivid base.
// During Fading: breath color blends toward the static color.
// Returns nil during Idle.
func (e *Engine) BorderColor(staticColor color.Color) color.Color {
	if e.state == AnimIdle {
		return nil
	}

	now := time.Now()
	breathT := e.breathValue(now)
	blended := e.from.BlendHcl(e.to, breathT)

	if e.state == AnimFading {
		fadeT := clamp01(float64(now.Sub(e.fadeStart)) / float64(e.fadeDuration))
		staticC, _ := colorful.MakeColor(staticColor)
		blended = staticC.BlendHcl(blended, 1.0-fadeT)
	}

	return blended
}

// BreathColor returns the breathing color at this instant, regardless of state.
// Used for input areas where breathing is driven by user typing, not the state machine.
func (e *Engine) BreathColor() color.Color {
	breathT := e.breathValue(time.Now())
	return e.from.BlendHcl(e.to, breathT)
}

// breathValue returns a 0..1 sinusoidal value based on elapsed time.
func (e *Engine) breathValue(now time.Time) float64 {
	elapsed := now.Sub(e.breathStart)
	t := float64(elapsed%e.breathPeriod) / float64(e.breathPeriod)
	return 0.5 + 0.5*math.Sin(t*2*math.Pi)
}
