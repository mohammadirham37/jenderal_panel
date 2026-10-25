package captcha

import (
	"bytes"
	"encoding/base64"
	"image/png"
	"strings"
	"testing"
	"time"
)

func TestEveryAlphabetCharacterHasAGlyph(t *testing.T) {
	for _, ch := range alphabet {
		glyph, ok := font[ch]
		if !ok {
			t.Fatalf("no glyph for %q", ch)
		}
		for _, row := range glyph {
			if len(row) != 5 {
				t.Fatalf("glyph %q row %q is not 5 wide", ch, row)
			}
		}
	}
}

func TestNewRendersAPNG(t *testing.T) {
	s := NewStore()
	c, err := s.New()
	if err != nil {
		t.Fatal(err)
	}
	raw, ok := strings.CutPrefix(c.Image, "data:image/png;base64,")
	if !ok {
		t.Fatalf("not a PNG data URL: %.40s", c.Image)
	}
	data, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if b := img.Bounds(); b.Dx() != width || b.Dy() != height {
		t.Errorf("image is %dx%d", b.Dx(), b.Dy())
	}
	if len(c.ID) != 32 {
		t.Errorf("id %q", c.ID)
	}
}

func answerOf(s *Store, id string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.entries[id].answer
}

func TestCheckIsCaseInsensitiveAndKeepsARightAnswerUntilConsumed(t *testing.T) {
	s := NewStore()
	c, _ := s.New()
	answer := answerOf(s, c.ID)
	if !s.Check(c.ID, " "+strings.ToLower(answer)+" ") {
		t.Fatal("right answer rejected")
	}
	// Still valid: the login may continue with a TOTP step.
	if !s.Check(c.ID, answer) {
		t.Fatal("right answer must stay valid until consumed")
	}
	s.Consume(c.ID)
	if s.Check(c.ID, answer) {
		t.Error("consumed challenge accepted")
	}
}

func TestWrongAnswerBurnsTheChallenge(t *testing.T) {
	s := NewStore()
	c, _ := s.New()
	answer := answerOf(s, c.ID)
	if s.Check(c.ID, "WRONG") {
		t.Fatal("wrong answer accepted")
	}
	if s.Check(c.ID, answer) {
		t.Error("a challenge must not survive a wrong guess")
	}
	if s.Check("unknown", answer) {
		t.Error("unknown id accepted")
	}
}

func TestExpiredChallengeIsRejected(t *testing.T) {
	s := NewStore()
	now := time.Now()
	s.now = func() time.Time { return now }
	c, _ := s.New()
	answer := answerOf(s, c.ID)
	s.now = func() time.Time { return now.Add(TTL + time.Second) }
	if s.Check(c.ID, answer) {
		t.Error("expired challenge accepted")
	}
}

func TestStoreIsBounded(t *testing.T) {
	s := NewStore()
	for i := 0; i < maxChallenges+50; i++ {
		s.mu.Lock()
		s.pruneLocked(time.Now())
		s.entries[strings.Repeat("x", 4)+string(rune('a'+i%26))+time.Now().Format("150405.000000000")+string(rune(i))] = entry{expires: time.Now().Add(TTL), created: time.Now()}
		s.mu.Unlock()
	}
	if len(s.entries) > maxChallenges {
		t.Errorf("store grew to %d", len(s.entries))
	}
}
