package trafficguard

import (
	"errors"
	"net/netip"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const maxLogLine = 64 * 1024

var combinedPattern = regexp.MustCompile(`^(\S+) \S+ \S+ \[([^\]]+)\] "((?:\\.|[^"])*)" ([0-9]{3}) (\S+) "((?:\\.|[^"])*)" "((?:\\.|[^"])*)"$`)

func ParseCombinedLog(line string) (LogEntry, error) {
	if len(line) == 0 || len(line) > maxLogLine {
		return LogEntry{}, errors.New("invalid Nginx log line length")
	}
	m := combinedPattern.FindStringSubmatch(line)
	if m == nil {
		return LogEntry{}, errors.New("malformed Nginx combined log line")
	}
	ip, err := netip.ParseAddr(m[1])
	if err != nil {
		return LogEntry{}, errors.New("invalid client IP")
	}
	stamp, err := time.Parse("02/Jan/2006:15:04:05 -0700", m[2])
	if err != nil {
		return LogEntry{}, errors.New("invalid log timestamp")
	}
	request := strings.SplitN(unescapeQuoted(m[3]), " ", 3)
	if len(request) != 3 {
		return LogEntry{}, errors.New("invalid request field")
	}
	status, _ := strconv.Atoi(m[4])
	if status < 100 || status > 599 {
		return LogEntry{}, errors.New("invalid HTTP status")
	}
	var bytes int64
	if m[5] != "-" {
		bytes, err = strconv.ParseInt(m[5], 10, 64)
		if err != nil || bytes < 0 {
			return LogEntry{}, errors.New("invalid response size")
		}
	}
	return LogEntry{IP: ip, Time: stamp.UTC(), Method: request[0], Path: request[1], Protocol: request[2], Status: status, Bytes: bytes, Referrer: unescapeQuoted(m[6]), UserAgent: unescapeQuoted(m[7])}, nil
}
func unescapeQuoted(v string) string {
	v = strings.ReplaceAll(v, `\"`, `"`)
	return strings.ReplaceAll(v, `\\`, `\`)
}
func NextOffset(cursor LogCursor, inode uint64, size int64) int64 {
	if cursor.Inode != inode || cursor.Offset < 0 || cursor.Offset > size {
		return 0
	}
	return cursor.Offset
}
