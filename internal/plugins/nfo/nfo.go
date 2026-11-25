package nfo

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/ambra-media/ambra/internal/plugins"
)

const maxNFOFileSize = 2 << 20 // 2 MiB

// Provider implements plugins.MetadataProvider for local Kodi/Emby .nfo files.
type Provider struct {
	enabled atomic.Bool
}

func New() *Provider {
	p := &Provider{}
	p.enabled.Store(true)
	return p
}

func (p *Provider) Manifest() plugins.Manifest {
	return plugins.Manifest{
		ID:          "ambra.builtin.nfo",
		Name:        "NFO Metadata Reader",
		Version:     "1.0.0",
		Description: "Reads local Kodi/Emby .nfo metadata files (<movie>, <tvshow>, <episodedetails>)",
		Author:      "Ambra",
		BuiltIn:     true,
		Enabled:     p.enabled.Load(),
		Priority:    10,
		Kinds:       []plugins.Kind{plugins.KindMetadata},
	}
}

func (p *Provider) Configure(cfg map[string]any) error {
	if v, ok := cfg["enabled"].(bool); ok {
		p.enabled.Store(v)
	}
	return nil
}

func readLimitedNFO(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if fi, err := f.Stat(); err == nil && fi.Size() > maxNFOFileSize {
		return nil, fmt.Errorf("nfo file exceeds maximum size of %d bytes", maxNFOFileSize)
	}
	data, err := io.ReadAll(io.LimitReader(f, maxNFOFileSize+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxNFOFileSize {
		return nil, fmt.Errorf("nfo file exceeds maximum size of %d bytes", maxNFOFileSize)
	}
	return data, nil
}

func (p *Provider) FetchMetadata(_ context.Context, q plugins.MediaQuery) (*plugins.MetadataResult, error) {
	if !p.enabled.Load() || q.Path == "" {
		return nil, nil
	}
	candidates := candidateNFOPaths(q.ItemType, q.Path)
	for _, nfoPath := range candidates {
		data, err := readLimitedNFO(nfoPath)
		if err != nil {
			continue
		}
		res, err := ParseNFO(data)
		if err != nil {
			continue
		}
		return res, nil
	}
	return nil, nil
}

func candidateNFOPaths(itemType, mediaPath string) []string {
	var out []string
	info, err := os.Stat(mediaPath)
	isDir := err == nil && info.IsDir()

	switch strings.ToLower(itemType) {
	case "series":
		if isDir {
			out = append(out, filepath.Join(mediaPath, "tvshow.nfo"))
		} else {
			out = append(out, filepath.Join(filepath.Dir(mediaPath), "tvshow.nfo"))
		}
	case "season":
		if isDir {
			out = append(out, filepath.Join(mediaPath, "season.nfo"))
		}
	case "episode":
		if !isDir {
			ext := filepath.Ext(mediaPath)
			out = append(out, strings.TrimSuffix(mediaPath, ext)+".nfo")
		}
	default: // Movie / generic
		if isDir {
			out = append(out, filepath.Join(mediaPath, "movie.nfo"))
		} else {
			ext := filepath.Ext(mediaPath)
			out = append(out, strings.TrimSuffix(mediaPath, ext)+".nfo")
			out = append(out, filepath.Join(filepath.Dir(mediaPath), "movie.nfo"))
		}
	}
	return out
}

type xmlUniqueID struct {
	Type    string `xml:"type,attr"`
	Default bool   `xml:"default,attr"`
	Value   string `xml:",chardata"`
}

type xmlRating struct {
	Name    string  `xml:"name,attr"`
	Default bool    `xml:"default,attr"`
	Value   float64 `xml:"value"`
}

type xmlRatings struct {
	Ratings []xmlRating `xml:"rating"`
}

type xmlActor struct {
	Name string `xml:"name"`
	Role string `xml:"role"`
}

type xmlNFO struct {
	XMLName       xml.Name      `xml:""`
	Title         string        `xml:"title"`
	OriginalTitle string        `xml:"originaltitle"`
	Plot          string        `xml:"plot"`
	Outline       string        `xml:"outline"`
	Tagline       string        `xml:"tagline"`
	Year          string        `xml:"year"`
	Premiered     string        `xml:"premiered"`
	Aired         string        `xml:"aired"`
	ReleaseDate   string        `xml:"releasedate"`
	Rating        string        `xml:"rating"`
	Ratings       xmlRatings    `xml:"ratings"`
	MPAA          string        `xml:"mpaa"`
	Genres        []string      `xml:"genre"`
	Studios       []string      `xml:"studio"`
	Tags          []string      `xml:"tag"`
	Actors        []xmlActor    `xml:"actor"`
	Directors     []string      `xml:"director"`
	Writers       []string      `xml:"writer"`
	Credits       []string      `xml:"credits"`
	TMDBID        string        `xml:"tmdbid"`
	IMDBID        string        `xml:"imdbid"`
	TVDBID        string        `xml:"tvdbid"`
	ID            string        `xml:"id"`
	UniqueIDs     []xmlUniqueID `xml:"uniqueid"`
}

// ParseNFO parses standard Kodi/Emby XML NFO content (<movie>, <tvshow>, <episodedetails>).
func ParseNFO(data []byte) (*plugins.MetadataResult, error) {
	// Strip UTF-8 BOM if present
	trimmed := strings.TrimPrefix(string(data), "\xef\xbb\xbf")
	var doc xmlNFO
	if err := xml.Unmarshal([]byte(trimmed), &doc); err != nil {
		return nil, err
	}

	res := &plugins.MetadataResult{
		Name:           strings.TrimSpace(doc.Title),
		OriginalTitle:  strings.TrimSpace(doc.OriginalTitle),
		Overview:       strings.TrimSpace(doc.Plot),
		Tagline:        strings.TrimSpace(doc.Tagline),
		OfficialRating: strings.TrimSpace(doc.MPAA),
		ProviderIDs:    make(map[string]string),
	}
	if res.Overview == "" {
		res.Overview = strings.TrimSpace(doc.Outline)
	}
	if y, err := strconv.Atoi(strings.TrimSpace(doc.Year)); err == nil && y > 0 {
		res.Year = y
	}
	for _, dateStr := range []string{doc.Premiered, doc.Aired, doc.ReleaseDate} {
		dateStr = strings.TrimSpace(dateStr)
		if dateStr != "" {
			res.PremiereDate = dateStr
			if res.Year == 0 && len(dateStr) >= 4 {
				if y, err := strconv.Atoi(dateStr[:4]); err == nil && y > 1800 {
					res.Year = y
				}
			}
			break
		}
	}

	if r, err := strconv.ParseFloat(strings.TrimSpace(doc.Rating), 64); err == nil && r > 0 {
		res.CommunityRating = r
	} else if len(doc.Ratings.Ratings) > 0 {
		for _, rt := range doc.Ratings.Ratings {
			if rt.Default && rt.Value > 0 {
				res.CommunityRating = rt.Value
				break
			}
		}
		if res.CommunityRating == 0 && doc.Ratings.Ratings[0].Value > 0 {
			res.CommunityRating = doc.Ratings.Ratings[0].Value
		}
	}

	for _, g := range doc.Genres {
		if g = strings.TrimSpace(g); g != "" {
			res.Genres = append(res.Genres, g)
		}
	}
	for _, s := range doc.Studios {
		if s = strings.TrimSpace(s); s != "" {
			res.Studios = append(res.Studios, s)
		}
	}
	for _, t := range doc.Tags {
		if t = strings.TrimSpace(t); t != "" {
			res.Tags = append(res.Tags, t)
		}
	}

	for _, d := range doc.Directors {
		if d = strings.TrimSpace(d); d != "" {
			res.People = append(res.People, plugins.PersonCredit{Name: d, Type: "Director"})
		}
	}
	for _, w := range append(doc.Writers, doc.Credits...) {
		if w = strings.TrimSpace(w); w != "" {
			res.People = append(res.People, plugins.PersonCredit{Name: w, Type: "Writer"})
		}
	}
	for _, a := range doc.Actors {
		if name := strings.TrimSpace(a.Name); name != "" {
			res.People = append(res.People, plugins.PersonCredit{
				Name: name,
				Role: strings.TrimSpace(a.Role),
				Type: "Actor",
			})
		}
	}

	if v := strings.TrimSpace(doc.TMDBID); v != "" {
		res.ProviderIDs["tmdb"] = v
	}
	if v := strings.TrimSpace(doc.IMDBID); v != "" {
		res.ProviderIDs["imdb"] = v
	}
	if v := strings.TrimSpace(doc.TVDBID); v != "" {
		res.ProviderIDs["tvdb"] = v
	}
	if v := strings.TrimSpace(doc.ID); v != "" && strings.HasPrefix(v, "tt") {
		if _, ok := res.ProviderIDs["imdb"]; !ok {
			res.ProviderIDs["imdb"] = v
		}
	}
	for _, uid := range doc.UniqueIDs {
		k := strings.ToLower(strings.TrimSpace(uid.Type))
		v := strings.TrimSpace(uid.Value)
		if k != "" && v != "" {
			res.ProviderIDs[k] = v
		}
	}
	return res, nil
}
