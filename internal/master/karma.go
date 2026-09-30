package master

import (
	"fmt"
	"math/rand/v2"
	"strings"

	"github.com/iamtew/ting/internal/karma"
	"github.com/iamtew/ting/internal/store"
)

func karmaLookup(d *store.DB, serverID int64, args []string) string {
	if len(args) == 0 {
		return karma.HelpText()
	}
	phrase := strings.Join(args, " ")
	score, found, err := d.GetKarma(serverID, phrase)
	if err != nil {
		return "karma unavailable: " + err.Error()
	}
	if !found {
		return fmt.Sprintf("%s: 0 (unknown)", phrase)
	}
	return fmt.Sprintf("%s: %d", phrase, score)
}

// karmaBumpReplies applies a channel bump. handled=false if the line is not karma syntax.
func karmaBumpReplies(d *store.DB, serverID int64, botNick, nick, text string) (lines []string, handled bool, err error) {
	bump, ok := karma.ParseBump(text)
	if !ok {
		return nil, false, nil
	}
	if bump.Reject != "" {
		return []string{bump.Reject}, true, nil
	}
	from, to, err := d.AddKarma(serverID, bump.Phrase, bump.Delta)
	if err != nil {
		return nil, true, err
	}
	if line := karma.AdjustReply(bump.Mode, bump.Result, from, to); line != "" {
		lines = append(lines, line)
	}
	if strings.EqualFold(strings.TrimSpace(bump.Phrase), botNick) {
		lines = append(lines,
			fmt.Sprintf("fuck yeah, I'm awesome! thank you %s! let's boost myself a bit more....", nick),
			botNick+"+2..20",
		)
		selfResult := 2 + rand.IntN(19)
		selfFrom, selfTo, err := d.AddKarma(serverID, bump.Phrase, selfResult)
		if err != nil {
			return lines, true, err
		}
		lines = append(lines, karma.AdjustReply(karma.ModeRandom, selfResult, selfFrom, selfTo))
	}
	return lines, true, nil
}
