// Package karma parses t3b-style phrase scores. Persistence lives in store.
package karma

import (
	"fmt"
	"math/rand/v2"
	"regexp"
	"strconv"
	"strings"
)

// Mode is how a bump was rolled (controls channel reply).
type Mode int

const (
	ModeSilent Mode = iota // ++ / --
	ModeDice               // +d / -d
	ModeRandom             // +N..M / -N..M
)

// MaxRandomBound is the highest allowed end of a +N..M range.
const MaxRandomBound = 23

// Bump is one parsed karma adjustment (or a rejection).
type Bump struct {
	Phrase string
	Delta  int  // signed change to apply
	Result int  // positive magnitude of the roll / step
	Mode   Mode // reply style
	Reject string
}

var (
	// +N..M / -N..M checked before ++/-- so foo+2..6 is random, foo++ stays ±1.
	reRandom = regexp.MustCompile(`^(.+?)([+-])(\d+)\.\.(\d+)`)
	reDice   = regexp.MustCompile(`(?i)^(.+?)([+-])d(?:\s|$)`)
	rePlain  = regexp.MustCompile(`^(.+?)(\+\+|--)`)
)

var partners = []string{
	"partner", "homie", "hermano", "muchacho", "bruv", "bro",
	"dude", "mfer", "stank-ass", "dipshit", "wappie",
}

// ParseBump finds a karma operator at the start of the line.
// ok=false means no karma syntax. Reject is set when the range is too large.
func ParseBump(text string) (Bump, bool) {
	text = strings.TrimSpace(text)
	if text == "" {
		return Bump{}, false
	}

	if m := reRandom.FindStringSubmatch(text); m != nil {
		phrase := strings.TrimSpace(m[1])
		if phrase == "" {
			return Bump{}, false
		}
		lo, _ := strconv.Atoi(m[3])
		hi, _ := strconv.Atoi(m[4])
		if lo > MaxRandomBound || hi > MaxRandomBound {
			return Bump{Phrase: phrase, Mode: ModeRandom, Reject: RejectMessage()}, true
		}
		if lo > hi {
			lo, hi = hi, lo
		}
		result := lo + rand.IntN(hi-lo+1)
		delta := result
		if m[2] == "-" {
			delta = -result
		}
		return Bump{Phrase: phrase, Delta: delta, Result: result, Mode: ModeRandom}, true
	}

	if m := rePlain.FindStringSubmatch(text); m != nil {
		phrase := strings.TrimSpace(m[1])
		if phrase == "" {
			return Bump{}, false
		}
		delta := 1
		if m[2] == "--" {
			delta = -1
		}
		return Bump{Phrase: phrase, Delta: delta, Result: 1, Mode: ModeSilent}, true
	}

	if m := reDice.FindStringSubmatch(text); m != nil {
		phrase := strings.TrimSpace(m[1])
		if phrase == "" {
			return Bump{}, false
		}
		result := 1 + rand.IntN(6)
		delta := result
		if m[2] == "-" {
			delta = -result
		}
		return Bump{Phrase: phrase, Delta: delta, Result: result, Mode: ModeDice}, true
	}

	return Bump{}, false
}

// RejectMessage is the "range too high" insult with a random partner word.
func RejectMessage() string {
	p := partners[rand.IntN(len(partners))]
	return fmt.Sprintf("woah there %s that's way too much karma there! hakuna yer tata's", p)
}

// AdjustReply formats the channel line for dice/random bumps; empty for silent.
func AdjustReply(mode Mode, result, from, to int) string {
	switch mode {
	case ModeDice:
		return fmt.Sprintf("karma adjusted with dice roll %d, new karma: %d -> %d", result, from, to)
	case ModeRandom:
		return fmt.Sprintf("karma adjusted with random %d, new karma: %d -> %d", result, from, to)
	default:
		return ""
	}
}

// HelpText is the bare .karma explainer.
func HelpText() string {
	return "karma: phrase++ / phrase-- (±1, silent); phrase+d / phrase-d (dice 1-6); phrase+N..M / phrase-N..M (random, max 23). .karma <phrase> looks up a score."
}
