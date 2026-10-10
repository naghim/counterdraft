package vision

import (
	"fmt"
	"image"
	"image/png"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"counterdraft/internal/config"
	"counterdraft/internal/heroes"
)

// These tests exercise the reader against a real Dota install when one is
// present, and skip otherwise. They are the only check that the VPK format and
// texture offsets this package assumes match what Valve actually ships; the
// synthetic fixtures elsewhere only pin the arithmetic.
func realDotaPath(t *testing.T) string {
	t.Helper()
	if p := os.Getenv("DOTA_PATH"); p != "" {
		if _, err := os.Stat(filepath.Join(p, "pak01_dir.vpk")); err == nil {
			return p
		}
	}
	if p, ok := config.FindDotaPath(); ok {
		if _, err := os.Stat(filepath.Join(p, "pak01_dir.vpk")); err == nil {
			return p
		}
	}
	t.Skip("no Dota install found; set DOTA_PATH to run")
	return ""
}

func TestRealVPKEnumeration(t *testing.T) {
	dota := realDotaPath(t)
	pak, err := OpenVPK(dota)
	if err != nil {
		t.Fatalf("OpenVPK(%s): %v", dota, err)
	}
	defer pak.Close()

	var heroEntries, vtexEntries int
	for _, e := range pak.Entries() {
		if strings.HasPrefix(e, VPKHeroDir+"/") && strings.Count(e, "/") == 3 {
			heroEntries++
			if strings.HasSuffix(e, textureSuffix) {
				vtexEntries++
			}
		}
	}
	t.Logf("hero-dir entries=%d, %s entries=%d", heroEntries, textureSuffix, vtexEntries)
	if vtexEntries == 0 {
		t.Errorf("no %s entries under %s/ - path/extension parsing is wrong", textureSuffix, VPKHeroDir)
	}
}

func TestRealVPKExtractAndDecode(t *testing.T) {
	dota := realDotaPath(t)
	ix, err := heroes.Load()
	if err != nil {
		t.Fatal(err)
	}
	list := make([]HeroImage, 0, len(ix.All()))
	for _, h := range ix.All() {
		list = append(list, HeroImage{ID: h.ID, Name: h.Name})
	}

	dir := t.TempDir()
	written, failed, err := ExtractVariants(list, dota, dir, nil)
	if err != nil {
		t.Fatalf("ExtractVariants: %v", err)
	}
	t.Logf("variants written=%d failed=%d", written, failed)
	if written == 0 {
		t.Fatal("no alternate portraits were extracted")
	}

	// Decode one known default texture and score it against the same hero's
	// CDN download. A wrong decoder still returns a picture; only the score
	// tells them apart.
	pak, err := OpenVPK(dota)
	if err != nil {
		t.Fatal(err)
	}
	defer pak.Close()
	img, err := decodeTexture(pak, VPKHeroDir+"/npc_dota_hero_antimage_png.vtex_c")
	if err != nil {
		t.Fatalf("decode Anti-Mage: %v", err)
	}

	cdn, err := fetchCDNPng(PortraitURL(HeroImage{ID: 1, Name: "npc_dota_hero_antimage"}))
	if err != nil {
		t.Logf("skipping CDN cross-check: %v", err)
		return
	}
	score := dotVec(descriptor(img), descriptor(cdn))
	t.Logf("decoded texture vs Valve CDN: %.4f", score)
	if score < 0.9 {
		t.Errorf("decode does not match the CDN (%.4f) - decoder is wrong", score)
	}
}

func dotVec(a, b []float32) float64 {
	sum := 0.0
	for i := range a {
		sum += float64(a[i]) * float64(b[i])
	}
	return sum
}

func fetchCDNPng(url string) (image.Image, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return png.Decode(resp.Body)
}
