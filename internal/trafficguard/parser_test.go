package trafficguard

import "testing"

func TestParseCombinedLogPreservesRequestAndStatus(t *testing.T) {
	got, err := ParseCombinedLog(`203.0.113.7 - - [09/Sep/2026:10:00:01 +0700] "GET /login?q=a HTTP/1.1" 429 123 "-" "Agent/1.0"`)
	if err != nil || got.IP.String() != "203.0.113.7" || got.Path != "/login?q=a" || got.Status != 429 {
		t.Fatalf("entry=%#v err=%v", got, err)
	}
}
func TestCursorRotationStartsNewInodeAtZero(t *testing.T) {
	if got := NextOffset(LogCursor{Inode: 10, Offset: 1000}, 11, 200); got != 0 {
		t.Fatalf("offset=%d", got)
	}
}
func TestParseCombinedLogRejectsMalformedInput(t *testing.T) {
	if _, err := ParseCombinedLog(`not a log`); err == nil {
		t.Fatal("malformed line accepted")
	}
}
