package vision

import (
	"fmt"
	"image"
	"image/png"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// HeroImage is the minimal hero metadata portrait building needs.
type HeroImage struct {
	ID   int    // stable Dota hero id
	Name string // internal entity name, e.g. "npc_dota_hero_antimage"
}

const (
	// cdnBase is Valve's CDN. The path is relative to it.
	cdnBase    = "https://cdn.cloudflare.steamstatic.com"
	cdnHeroes  = "/apps/dota2/images/dota_react/heroes/"
	internalPr = "npc_dota_hero_"

	// textureSuffix is the compiled landscape portrait's extension.
	textureSuffix = "_png.vtex_c"

	// downloadDelay is polite spacing; this is a CDN rather than an API, so it
	// is about not looking like a scraper, not about a documented limit.
	downloadDelay = 50 * time.Millisecond
)

// PortraitURL works out where a hero's default portrait lives, derived from the
// internal name rather than fetched from a second endpoint.
func PortraitURL(hero HeroImage) string {
	if !strings.HasPrefix(hero.Name, internalPr) {
		return ""
	}
	short := strings.TrimPrefix(hero.Name, internalPr)
	return fmt.Sprintf("%s%s%s.png", cdnBase, cdnHeroes, short)
}

// EnsureBuild describes what one pass over both sources did.
type EnsureBuild struct {
	Downloaded   int
	DownloadFail int
	Extracted    int
	ExtractFail  int
	Heroes       int
	Variants     int
	DotaFound    bool
	Error        string
}

// Usable is true when there is enough art to recognise anything at all.
func (b EnsureBuild) Usable() bool { return b.Heroes > 0 }

// Changed is true when this pass actually wrote something.
func (b EnsureBuild) Changed() bool { return b.Downloaded > 0 || b.Extracted > 0 }

// Download fetches the landscape art of every hero that has none stored yet.
// It returns (fetched, failed). A hero that fails is left missing rather than
// written empty, so a later run retries it.
func Download(heroes []HeroImage, directory string, onHero func(name, note string)) (int, int, error) {
	if directory == "" {
		directory = PortraitDir()
	}
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return 0, 0, err
	}

	client := &http.Client{Timeout: 30 * time.Second}
	fetched, failed := 0, 0

	for _, hero := range heroes {
		if hero.ID <= 0 {
			continue
		}
		url := PortraitURL(hero)
		if url == "" {
			continue
		}
		path := filepath.Join(directory, fmt.Sprintf("%d.png", hero.ID))
		if _, err := os.Stat(path); err == nil {
			continue
		}

		note := "ok"
		if err := fetchPortrait(client, url, path); err != nil {
			failed++
			note = "failed: " + err.Error()
		} else {
			fetched++
		}
		if onHero != nil {
			label := hero.Name
			if label == "" {
				label = fmt.Sprint(hero.ID)
			}
			onHero(label, note)
		}
		time.Sleep(downloadDelay)
	}
	return fetched, failed, nil
}

func fetchPortrait(client *http.Client, url, path string) error {
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	tmp := path + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := f.ReadFrom(resp.Body); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}

// ExtractVariants pulls every alternate hero portrait out of the installed game.
// It returns (written, failed). Files are named "{id}_{variant}.png", because an
// alternate portrait is not another hero: it resolves to the id it belongs to.
//
// Arcanas, immortal sets and personas turn up under the suffixes alt, alt1,
// alt2, alt3, persona1 and carnival, so nothing here is hard-coded: every file
// whose name begins with a hero's internal name plus an underscore is taken.
func ExtractVariants(heroes []HeroImage, dotaPath, directory string, onVariant func(name, variant, note string)) (int, int, error) {
	if directory == "" {
		directory = PortraitDir()
	}
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return 0, 0, err
	}

	pak, err := OpenVPK(dotaPath)
	if err != nil {
		return 0, 0, err
	}
	defer pak.Close()

	byName := map[string]HeroImage{}
	for _, hero := range heroes {
		if hero.Name != "" && hero.ID > 0 {
			byName[hero.Name] = hero
		}
	}
	ordered := make([]string, 0, len(byName))
	for name := range byName {
		ordered = append(ordered, name)
	}
	// Longest-name-first: no hero's internal name is a prefix of another's
	// today, but the ordering costs nothing and stops the day one is.
	sort.Slice(ordered, func(i, j int) bool {
		if len(ordered[i]) != len(ordered[j]) {
			return len(ordered[i]) > len(ordered[j])
		}
		return ordered[i] < ordered[j]
	})

	written, failed := 0, 0
	for _, source := range pak.Entries() {
		if !strings.HasPrefix(source, VPKHeroDir+"/") || strings.Count(source, "/") != 3 {
			continue
		}
		stem := source[strings.LastIndex(source, "/")+1:]
		if !strings.HasSuffix(stem, textureSuffix) {
			continue
		}
		stem = strings.TrimSuffix(stem, textureSuffix)

		for _, name := range ordered {
			// An exact hero name is the default portrait, which comes from the
			// CDN. Only a name followed by a suffix is an alternate.
			if !strings.HasPrefix(stem, name+"_") {
				continue
			}
			hero := byName[name]
			variant := stem[len(name)+1:]
			target := filepath.Join(directory, fmt.Sprintf("%d_%s.png", hero.ID, variant))
			if _, err := os.Stat(target); err == nil {
				break
			}
			note := "ok"
			if img, err := decodeTexture(pak, source); err != nil {
				failed++
				note = "failed: " + err.Error()
			} else if err := savePNG(img, target); err != nil {
				failed++
				note = "failed: " + err.Error()
			} else {
				written++
			}
			if onVariant != nil {
				onVariant(name, variant, note)
			}
			break
		}
	}
	return written, failed, nil
}

func decodeTexture(pak *VPK, path string) (image.Image, error) {
	raw, err := pak.Read(path)
	if err != nil {
		return nil, err
	}
	return decodeVTEX(raw)
}

// StoredCounts returns how many default portraits and how many alternates are
// on disk.
func StoredCounts(directory string) (defaults, alternates int) {
	if directory == "" {
		directory = PortraitDir()
	}
	entries, err := os.ReadDir(directory)
	if err != nil {
		return 0, 0
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".png") {
			continue
		}
		stem := strings.TrimSuffix(e.Name(), ".png")
		if HeroIDOf(stem) == 0 {
			continue
		}
		if strings.Contains(stem, "_") {
			alternates++
		} else {
			defaults++
		}
	}
	return defaults, alternates
}

// Ensure makes sure the reference art exists, from whichever sources are
// reachable. It never fails outright: a missing network leaves whatever is
// already on disk; a missing Dota install leaves the defaults and refuses the
// handful of slots wearing an arcana.
func Ensure(heroes []HeroImage, directory, dotaPath string) EnsureBuild {
	if directory == "" {
		directory = PortraitDir()
	}
	build := EnsureBuild{DotaFound: dotaPath != ""}

	downloaded, failed, err := Download(heroes, directory, nil)
	if err != nil {
		build.Error = err.Error()
		return build
	}
	build.Downloaded, build.DownloadFail = downloaded, failed

	if dotaPath != "" {
		extracted, efailed, err := ExtractVariants(heroes, dotaPath, directory, nil)
		if err != nil {
			// The defaults are written by this point and are the half that
			// matters.
			build.Error = err.Error()
		} else {
			build.Extracted, build.ExtractFail = extracted, efailed
		}
	}

	build.Heroes, build.Variants = StoredCounts(directory)
	return build
}

func savePNG(img image.Image, path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}
