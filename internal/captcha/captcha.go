// Package captcha issues self-hosted image challenges for the panel login.
// Challenges are rendered locally from a built-in bitmap font, so the panel
// needs no third-party service, API key, or internet access, and no login
// data leaves the server.
package captcha

import (
	"bytes"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math/big"
	"strings"
	"sync"
	"time"
)

const (
	// Length is the number of characters in a challenge.
	Length = 5
	// TTL is how long a challenge can be answered.
	TTL = 5 * time.Minute
	// maxChallenges bounds memory when the public endpoint is hammered; the
	// oldest challenges are dropped first.
	maxChallenges = 5000

	width  = 168
	height = 56
	scale  = 4
)

// alphabet leaves out characters that are easy to confuse (0/O, 1/I/L, 8/B,
// S/5, Z/2, Q).
const alphabet = "ACDEFGHJKMNPRTUVWXY2345679"

// Challenge is what the login page shows: an opaque id and the image.
type Challenge struct {
	ID    string `json:"id"`
	Image string `json:"image"` // data:image/png;base64,...
}

type entry struct {
	answer  string
	expires time.Time
	created time.Time
}

// Store keeps issued challenges in memory. Answers are single-use.
type Store struct {
	mu      sync.Mutex
	entries map[string]entry
	now     func() time.Time
}

// NewStore creates an empty challenge store.
func NewStore() *Store {
	return &Store{entries: make(map[string]entry), now: time.Now}
}

// New issues a challenge.
func (s *Store) New() (Challenge, error) {
	answer, err := randomText(Length)
	if err != nil {
		return Challenge{}, err
	}
	img, err := render(answer)
	if err != nil {
		return Challenge{}, err
	}
	idBytes := make([]byte, 16)
	if _, err := rand.Read(idBytes); err != nil {
		return Challenge{}, fmt.Errorf("captcha id: %w", err)
	}
	id := hex.EncodeToString(idBytes)

	now := s.now()
	s.mu.Lock()
	s.pruneLocked(now)
	s.entries[id] = entry{answer: answer, expires: now.Add(TTL), created: now}
	s.mu.Unlock()

	return Challenge{ID: id, Image: "data:image/png;base64," + base64.StdEncoding.EncodeToString(img)}, nil
}

// Check reports whether answer solves challenge id. A wrong answer burns the
// challenge so it cannot be guessed repeatedly; a right one stays valid
// until Consume so a login that continues with a TOTP step can reuse it.
func (s *Store) Check(id, answer string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[id]
	if !ok {
		return false
	}
	if s.now().After(e.expires) {
		delete(s.entries, id)
		return false
	}
	given := strings.ToUpper(strings.TrimSpace(answer))
	if subtle.ConstantTimeCompare([]byte(given), []byte(e.answer)) != 1 {
		delete(s.entries, id)
		return false
	}
	return true
}

// Consume removes a challenge once the login attempt it guarded is over.
func (s *Store) Consume(id string) {
	s.mu.Lock()
	delete(s.entries, id)
	s.mu.Unlock()
}

// pruneLocked drops expired challenges and, past the cap, the oldest ones.
func (s *Store) pruneLocked(now time.Time) {
	for id, e := range s.entries {
		if now.After(e.expires) {
			delete(s.entries, id)
		}
	}
	for len(s.entries) >= maxChallenges {
		var oldestID string
		var oldest time.Time
		for id, e := range s.entries {
			if oldestID == "" || e.created.Before(oldest) {
				oldestID, oldest = id, e.created
			}
		}
		delete(s.entries, oldestID)
	}
}

func randomText(n int) (string, error) {
	var b strings.Builder
	for i := 0; i < n; i++ {
		idx, err := randInt(len(alphabet))
		if err != nil {
			return "", err
		}
		b.WriteByte(alphabet[idx])
	}
	return b.String(), nil
}

func randInt(n int) (int, error) {
	v, err := rand.Int(rand.Reader, big.NewInt(int64(n)))
	if err != nil {
		return 0, fmt.Errorf("captcha random: %w", err)
	}
	return int(v.Int64()), nil
}

// mustRand is randInt for rendering noise, where a failed read only means
// less noise.
func mustRand(n int) int {
	v, err := randInt(n)
	if err != nil {
		return 0
	}
	return v
}

// render draws the text with per-glyph jitter and shear over line and dot
// noise.
func render(text string) ([]byte, error) {
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	bg := color.NRGBA{R: uint8(235 + mustRand(20)), G: uint8(235 + mustRand(20)), B: uint8(235 + mustRand(20)), A: 255}
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, bg)
		}
	}

	// Background noise lines in pale colors.
	for i := 0; i < 6; i++ {
		c := color.NRGBA{R: uint8(140 + mustRand(80)), G: uint8(140 + mustRand(80)), B: uint8(140 + mustRand(80)), A: 255}
		drawLine(img, mustRand(width), mustRand(height), mustRand(width), mustRand(height), c)
	}

	glyphWidth := 5*scale + 6
	startX := (width - glyphWidth*len(text)) / 2
	for i, ch := range text {
		glyph := font[ch]
		ink := color.NRGBA{R: uint8(mustRand(110)), G: uint8(mustRand(110)), B: uint8(mustRand(110)), A: 255}
		ox := startX + i*glyphWidth + mustRand(5) - 2
		oy := (height-7*scale)/2 + mustRand(11) - 5
		shear := mustRand(5) - 2 // horizontal shift per row block, -2..2 px
		for row := 0; row < 7; row++ {
			for col := 0; col < 5; col++ {
				if glyph[row][col] != '#' {
					continue
				}
				for dy := 0; dy < scale; dy++ {
					for dx := 0; dx < scale; dx++ {
						x := ox + col*scale + dx + (row-3)*shear
						y := oy + row*scale + dy
						if x >= 0 && x < width && y >= 0 && y < height {
							img.Set(x, y, ink)
						}
					}
				}
			}
		}
	}

	// Foreground strike lines and speckles across the glyphs.
	for i := 0; i < 3; i++ {
		c := color.NRGBA{R: uint8(mustRand(140)), G: uint8(mustRand(140)), B: uint8(mustRand(140)), A: 255}
		drawLine(img, 0, mustRand(height), width-1, mustRand(height), c)
	}
	for i := 0; i < 400; i++ {
		c := color.NRGBA{R: uint8(mustRand(256)), G: uint8(mustRand(256)), B: uint8(mustRand(256)), A: 255}
		img.Set(mustRand(width), mustRand(height), c)
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, fmt.Errorf("encode captcha: %w", err)
	}
	return buf.Bytes(), nil
}

// drawLine draws a 1px Bresenham line.
func drawLine(img *image.NRGBA, x0, y0, x1, y1 int, c color.Color) {
	dx, dy := abs(x1-x0), -abs(y1-y0)
	sx, sy := 1, 1
	if x0 > x1 {
		sx = -1
	}
	if y0 > y1 {
		sy = -1
	}
	err := dx + dy
	for {
		img.Set(x0, y0, c)
		if x0 == x1 && y0 == y1 {
			return
		}
		e2 := 2 * err
		if e2 >= dy {
			err += dy
			x0 += sx
		}
		if e2 <= dx {
			err += dx
			y0 += sy
		}
	}
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// font is a 5x7 bitmap for every character in alphabet.
var font = map[rune][7]string{
	'A': {".###.", "#...#", "#...#", "#####", "#...#", "#...#", "#...#"},
	'C': {".####", "#....", "#....", "#....", "#....", "#....", ".####"},
	'D': {"####.", "#...#", "#...#", "#...#", "#...#", "#...#", "####."},
	'E': {"#####", "#....", "#....", "####.", "#....", "#....", "#####"},
	'F': {"#####", "#....", "#....", "####.", "#....", "#....", "#...."},
	'G': {".####", "#....", "#....", "#.###", "#...#", "#...#", ".####"},
	'H': {"#...#", "#...#", "#...#", "#####", "#...#", "#...#", "#...#"},
	'J': {"..###", "...#.", "...#.", "...#.", "...#.", "#..#.", ".##.."},
	'K': {"#...#", "#..#.", "#.#..", "##...", "#.#..", "#..#.", "#...#"},
	'M': {"#...#", "##.##", "#.#.#", "#.#.#", "#...#", "#...#", "#...#"},
	'N': {"#...#", "##..#", "#.#.#", "#..##", "#...#", "#...#", "#...#"},
	'P': {"####.", "#...#", "#...#", "####.", "#....", "#....", "#...."},
	'R': {"####.", "#...#", "#...#", "####.", "#.#..", "#..#.", "#...#"},
	'T': {"#####", "..#..", "..#..", "..#..", "..#..", "..#..", "..#.."},
	'U': {"#...#", "#...#", "#...#", "#...#", "#...#", "#...#", ".###."},
	'V': {"#...#", "#...#", "#...#", "#...#", "#...#", ".#.#.", "..#.."},
	'W': {"#...#", "#...#", "#...#", "#.#.#", "#.#.#", "##.##", "#...#"},
	'X': {"#...#", "#...#", ".#.#.", "..#..", ".#.#.", "#...#", "#...#"},
	'Y': {"#...#", "#...#", ".#.#.", "..#..", "..#..", "..#..", "..#.."},
	'2': {".###.", "#...#", "....#", "...#.", "..#..", ".#...", "#####"},
	'3': {"####.", "....#", "....#", ".###.", "....#", "....#", "####."},
	'4': {"...#.", "..##.", ".#.#.", "#..#.", "#####", "...#.", "...#."},
	'5': {"#####", "#....", "####.", "....#", "....#", "#...#", ".###."},
	'6': {".###.", "#....", "#....", "####.", "#...#", "#...#", ".###."},
	'7': {"#####", "....#", "...#.", "..#..", ".#...", ".#...", ".#..."},
	'9': {".###.", "#...#", "#...#", ".####", "....#", "....#", ".###."},
}

// AnswerForTest returns a challenge's answer. It exists for tests in other
// packages that drive a full login.
func (s *Store) AnswerForTest(id string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.entries[id].answer
}
