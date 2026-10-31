package logs

import (
	"regexp"
	"strings"
)

// journalLine is one raw core log line with its timestamp in
// milliseconds. The journal reader is gone (the panel now follows its
// own log file), but the name stays: the file follower feeds lines
// through this same parser with a timestamp parsed from the line text.
type journalLine struct {
	tsMs    int64
	message string
}

// singBoxLine matches the core's own log format:
// "+0000 2026-10-24 08:01:11 INFO [1285601736 163ms] inbound/...: ...".
// The offset is the core host's local timezone (+0000 on a UTC host,
// +0800 observed on a Shanghai host), so it is matched, never assumed.
var singBoxLine = regexp.MustCompile(
	`^[+-]\d{4} \d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2} (DEBUG|INFO|WARN|ERROR|FATAL) (.*)$`)

// connIDPrefix strips the "[connection-id elapsed]" token sing-box
// prepends to connection log lines; the id itself groups one
// connection's lines (from / to / outbound) into a single record.
var connIDPrefix = regexp.MustCompile(`^\[[^\]]+\]\s*`)
var connIDToken = regexp.MustCompile(`^\[(\d+) [^\]]+\]`)

// Connection lines are the core's per-connection records; everything
// else is a core system event.
func isConnectionLine(msg string) bool {
	return strings.Contains(msg, "inbound connection") ||
		strings.Contains(msg, "outbound connection")
}

var (
	connTagRe  = regexp.MustCompile(`(?:inbound|outbound)/[\w-]+\[([^\]]+)\]`)
	connFromRe = regexp.MustCompile(`inbound connection from (\S+)`)
	connToRe   = regexp.MustCompile(`(?:inbound|outbound) connection to (\S+)`)
	connUserRe = regexp.MustCompile(`\[([^\]]+)\] inbound connection to`)
)

// ansiCode matches terminal color sequences (sing-box colorizes its
// level and connection-id tokens on stderr; file output observed so
// far is plain, but stripping is harmless and protects the parser).
var ansiCode = regexp.MustCompile("\x1b\\[[0-9;?]*[a-zA-Z]")

func stripANSI(s string) string { return ansiCode.ReplaceAllString(s, "") }

// mergeConnection folds one connection line into the record built from
// its sibling lines: origin IP (from-line), subscription (user on the
// to-line), node (inbound tag) and destination (to-line). The outbound
// line's tag is "direct"; the inbound line's tag is the node, so
// inbound tags always win.
func mergeConnection(dst, src *Entry) {
	if src.Ts < dst.Ts {
		dst.Ts = src.Ts
	}
	if src.Source != "" {
		dst.Source = src.Source
	}
	if src.Destination != "" {
		dst.Destination = src.Destination
	}
	if src.User != "" {
		dst.User = src.User
	}
	if src.Tag != "" {
		if !strings.HasPrefix(src.Message, "outbound/") || dst.Tag == "" {
			dst.Tag = src.Tag
		}
	}
}

// parseJournalLine splits a core log line into a log entry. Lines that
// do not match the core format keep the caller-supplied timestamp, an
// empty level, and the raw message.
func parseJournalLine(l journalLine) Entry {
	m := singBoxLine.FindStringSubmatch(l.message)
	level, content := "", l.message
	if m != nil {
		level, content = m[1], m[2]
	}
	e := Entry{Ts: l.tsMs, Level: level, Message: content}
	if m != nil {
		if id := connIDToken.FindStringSubmatch(content); id != nil {
			e.ConnID = id[1]
		}
		content = connIDPrefix.ReplaceAllString(content, "")
		e.Message = content
	}
	if !isConnectionLine(content) {
		e.Category = "core"
		return e
	}
	e.Category = "connection"
	if m := connTagRe.FindStringSubmatch(content); m != nil {
		e.Tag = m[1]
	}
	if m := connFromRe.FindStringSubmatch(content); m != nil {
		e.Source = m[1]
	}
	if m := connToRe.FindStringSubmatch(content); m != nil {
		e.Destination = m[1]
	}
	if m := connUserRe.FindStringSubmatch(content); m != nil {
		e.User = m[1]
	}
	return e
}
